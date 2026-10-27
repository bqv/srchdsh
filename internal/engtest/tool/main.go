// srchdsh/internal/engtest/tool is a very rudimentary tool to
// extract requests and responses to search engines from their test files.
package main

import (
	"encoding/gob"
	"flag"
	"fmt"
	"os"

	"srchdsh/internal/engtest"
)

var (
	extractRequest = flag.Bool("req", false, "extract the request instead of the response")
	includeHeaders = flag.Bool("headers", false, "include headers in output")
)

// The following is not written ideally and was just quickly thrown together in
// the span of 10 minutes.
func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "expected engtest data file as argument\n")
		os.Exit(1)
	}

	// Load and decode the test file
	h, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open %q: %v\n", flag.Arg(0), err)
		os.Exit(1)
	}
	defer h.Close()

	data := engtest.TestData{}
	if err := gob.NewDecoder(h).Decode(&data); err != nil {
		fmt.Fprintf(os.Stderr, "failed to read test data: %v\n", err)
		os.Exit(1)
	}

	if *extractRequest {
		if *includeHeaders {
			for _, v := range data.Req.Headers {
				fmt.Printf("%s: %s\n", v.Key, v.Value)
			}
			fmt.Println()
		}

		// Print body
		os.Stdout.Write(data.Req.Body)
		return
	}

	if *includeHeaders {
		fmt.Printf("Status: %d\n", data.Res.Status)
		for _, v := range data.Res.Headers {
			fmt.Printf("%s: %s\n", v.Key, v.Value)
		}
		fmt.Println()
	}

	// Print body
	os.Stdout.Write(data.Res.Body)
}
