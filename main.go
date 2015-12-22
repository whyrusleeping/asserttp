package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type TestConf struct {
	Tests []Assertion

	ListenPort int
	TargetPort int

	Timeout time.Duration

	Commands []string
}

type Assertion struct {
	RequiredHeaders map[string][]string

	ExpectedPath string

	BodyFilter string
}

func (tc *TestConf) Run() error {
	list, err := net.Listen("tcp", fmt.Sprintf(":%d", tc.ListenPort))
	if err != nil {
		return err
	}

	port := list.Addr().(*net.TCPAddr).Port

	var assertCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("running test %d\n", assertCount)
		a := tc.Tests[assertCount]
		assertCount++

		if a.ExpectedPath != "" && r.URL.Path != a.ExpectedPath {
			fmt.Fprintf(os.Stderr, "expected request to %q, got %q", a.ExpectedPath, r.URL.Path)
			os.Exit(1)
		}

		for k, v := range a.RequiredHeaders {
			f, ok := r.Header[k]
			if !ok {
				fmt.Fprintf(os.Stderr, "header %s not found\n", k)
				os.Exit(1)
			}

			if !stringArrMatch(f, v) {
				fmt.Fprintf(os.Stderr, "header %s had incorrect value [%s != %s]", k, f, v)
				os.Exit(1)
			}
		}

		r.RequestURI = ""
		r.URL.Host = fmt.Sprintf("localhost:%d", tc.TargetPort)
		r.URL.Scheme = "http"
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			fatal(err)
		}

		c, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			fatal(err)
		}

		err = resp.Write(c)
		if err != nil {
			fatal(err)
		}

		fmt.Printf("passed %d/%d\n", assertCount, len(tc.Tests))
		if assertCount == len(tc.Tests) {
			list.Close()
		}
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		http.Serve(list, mux)
	}()

	var timoutCh <-chan time.Time
	if tc.Timeout != 0 {
		timoutCh = time.After(tc.Timeout)
	}

	for _, c := range tc.Commands {
		c = strings.Replace(c, "%port", fmt.Sprint(port), -1)
		fmt.Println("cmd:", c)
		args := strings.Fields(c)
		cmd := exec.Command(args[0], args[1:]...)
		err = cmd.Run()
		if err != nil {
			fatal(err)
		}
	}

	select {
	case <-done:
		return nil
	case <-timoutCh:
		fatal("test timed out!")
	}
	panic("should not get here")
}

func stringArrMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fatal(i interface{}) {
	fmt.Fprintln(os.Stderr, i)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		fatal("must specify a testing json file as input")
	}

	fi, err := os.Open(os.Args[1])
	if err != nil {
		fatal(err)
	}

	var tc TestConf
	err = json.NewDecoder(fi).Decode(&tc)
	if err != nil {
		fatal(err)
	}

	err = tc.Run()
	if err != nil {
		fatal(err)
	}
}
