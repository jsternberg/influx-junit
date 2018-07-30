package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"time"

	influxdb "github.com/influxdata/influxdb/client/v2"
	"github.com/spf13/pflag"
)

type TestSuites struct {
	XMLName xml.Name    `xml:"testsuites"`
	Items   []TestSuite `xml:"testsuite"`
}

type TestSuite struct {
	Tests      int        `xml:"tests,attr"`
	Failures   int        `xml:"failures,attr"`
	Duration   float64    `xml:"time,attr"`
	Name       string     `xml:"name,attr"`
	Properties Properties `xml:"properties"`
	TestCases  []TestCase `xml:"testcase"`
}

type Properties struct {
	Items []Property `xml:"property"`
}

type Property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type TestCase struct {
	ClassName string  `xml:"classname,attr"`
	Name      string  `xml:"name,attr"`
	Duration  float64 `xml:"time,attr"`
}

type PointsWriter interface {
	Write(pt *influxdb.Point) error
	Flush() error
}

type printPointsWriter struct {
	w io.Writer
}

func (pw *printPointsWriter) Write(pt *influxdb.Point) error {
	fmt.Fprintln(pw.w, pt.String())
	return nil
}

func (pw *printPointsWriter) Flush() error {
	return nil
}

type influxdbPointsWriter struct {
	client influxdb.Client
	bp     influxdb.BatchPoints
}

func (pw *influxdbPointsWriter) Write(pt *influxdb.Point) error {
	pw.bp.AddPoint(pt)
	return nil
}

func (pw *influxdbPointsWriter) Flush() error {
	return pw.client.Write(pw.bp)
}

func main() {
	host := pflag.StringP("host", "H", "http://localhost:8086", "influxdb server to write to")
	db := pflag.StringP("database", "d", "", "influxdb database")
	rp := pflag.StringP("retention-policy", "r", "", "influxdb retention policy")
	print := pflag.Bool("print", false, "print the line protocol instead of writing to the server")
	pflag.Parse()

	args := pflag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: Must specify at least one argument.\n")
		os.Exit(1)
	}

	var pw PointsWriter
	if *print {
		pw = &printPointsWriter{w: os.Stdout}
	} else {
		client, err := influxdb.NewHTTPClient(influxdb.HTTPConfig{
			Addr: *host,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Could not create HTTP client: %s.\n", err)
			os.Exit(1)
		}

		bp, err := influxdb.NewBatchPoints(influxdb.BatchPointsConfig{
			Database:        *db,
			RetentionPolicy: *rp,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Could not create batch points: %s.\n", err)
			os.Exit(1)
		}
		pw = &influxdbPointsWriter{
			client: client,
			bp:     bp,
		}
	}

	now := time.Now()
	for _, arg := range args {
		f, err := os.Open(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Unable to open file: %s.\n", err)
			os.Exit(1)
		}

		var tests TestSuites
		dec := xml.NewDecoder(f)
		if err := dec.Decode(&tests); err != nil {
			fmt.Fprintf(os.Stderr, "Error: Unable to decode file %s: %s.\n", arg, err)
			f.Close()
			os.Exit(1)
		}
		f.Close()

		for _, testsuite := range tests.Items {
			for _, testcase := range testsuite.TestCases {
				pt, err := influxdb.NewPoint("junit_test_results",
					map[string]string{
						"suite_name": testsuite.Name,
						"test_name":  testcase.Name,
					},
					map[string]interface{}{
						"duration": testcase.Duration,
					},
					now,
				)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: Could not create point: %s.\n", err)
					os.Exit(1)
				}
				if err := pw.Write(pt); err != nil {
					fmt.Fprintf(os.Stderr, "Error: Could not write point: %s.\n", err)
					os.Exit(1)
				}
			}
		}

		if err := pw.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: Could not write points: %s.\n", err)
			os.Exit(1)
		}
	}
}
