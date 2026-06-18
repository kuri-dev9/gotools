package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gtools/pkg/cli"
	"gtools/pkg/netutil"
	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

const version_info = "1.0.1"

func init() {
    runtime.GOMAXPROCS(1)
}

func main() {
	// =====================
	// flags
	// =====================
	method := pflag.StringP("request", "X", "GET", "HTTP method")
	data := pflag.StringP("data", "d", "", "request body or @file")
	insecure := pflag.BoolP("insecure", "k", false, "allow insecure TLS")
	resolve := pflag.String("resolve", "", "host:port:ip")
	caCert := pflag.String("cacert", "", "CA cert file")
	certFile := pflag.String("cert", "", "client cert file")
	keyFile := pflag.String("key", "", "client key file")

	headers := pflag.StringArrayP("header", "H", nil, "request headers")

	verboseFlag := pflag.BoolP("verbose", "v", false, "verbose output")
	includeHeader := pflag.BoolP("include", "i", false, "include response headers")
	silent := pflag.BoolP("silent", "s", false, "silent mode")

	output := pflag.StringP("output", "o", "", "output file")

	retry := pflag.Int("retry", 0, "retry count")
	retryDelay := pflag.Int("retry-delay", 1, "retry delay seconds")

	timeout := pflag.Int("timeout", 30, "timeout seconds")
	writeOut := pflag.StringP("write-out", "w", "", "write-out format")

	followRedirect := pflag.BoolP("location", "L", false, "follow redirect")

	helpFlag := pflag.BoolP("help", "h", false, "show help")
	versionFlag := pflag.BoolP("version", "V", false, "show version")

	pflag.Usage = func() {
		cli.PrintHelp("gcurl", helpText)
	}

	pflag.Parse()

	// =====================
	// help / validation
	// =====================
	if *helpFlag {
		cli.PrintHelp("gcurl", helpText)
		return
	}

	if *versionFlag {
		version.Version = version_info
		version.Print()
		return
	}

	if pflag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "error: missing url\n")
		cli.PrintHelp("gcurl", helpText)
		os.Exit(1)
	}

	rawURL := pflag.Arg(0)

	if _, err := url.ParseRequestURI(rawURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid url: %v\n\n", err)
		cli.PrintHelp("gcurl", helpText)
		os.Exit(1)
	}

	// =====================
	// resolve / TLS
	// =====================
	resolveInfo, err := netutil.ParseResolve(*resolve)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --resolve: %v\n\n", err)
		cli.PrintHelp("gcurl", helpText)
		os.Exit(1)
	}

	tlsCfg, err := netutil.BuildTLSConfig(netutil.TLSOptions{
		Insecure: *insecure,
		CACert:   *caCert,
		CertFile: *certFile,
		KeyFile:  *keyFile,
	}, resolveInfo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: tls config: %v\n\n", err)
		cli.PrintHelp("gcurl", helpText)
		os.Exit(1)
	}

	tr := netutil.BuildTransport(tlsCfg, resolveInfo)

	client := &http.Client{
		Timeout: time.Duration(*timeout) * time.Second,
		Transport: tr,
	}

	if !*followRedirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}


	var body []byte
	if *data != "" {
		if strings.HasPrefix(*data, "@") {
			b, err := ioutil.ReadFile(strings.TrimPrefix(*data, "@"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: read data file: %v\n", err)
				os.Exit(1)
			}
			body = b
		} else {
			body = []byte(*data)
		}
	}

	// =====================
	// retry loop
	// =====================
	var resp *http.Response
	var reqErr error

	start := time.Now()

	for i := 0; i <= *retry; i++ {

		req, err := http.NewRequest(strings.ToUpper(*method), rawURL, bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "request build error: %v\n", err)
			os.Exit(1)
		}

		// headers
		for _, h := range *headers {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}

		// verbose request
		if *verboseFlag {
			fmt.Fprintf(os.Stderr, "> %s %s\n", req.Method, req.URL)
			for k, v := range req.Header {
				fmt.Fprintf(os.Stderr, "> %s: %s\n", k, v)
			}
			fmt.Fprintln(os.Stderr)
		}

		resp, reqErr = client.Do(req)

		if reqErr == nil && resp.StatusCode < 500 {
			break
		}

		if i < *retry {
			time.Sleep(time.Duration(*retryDelay) * time.Second)
		}
	}

	duration := time.Since(start).Seconds()

	if reqErr != nil {
		if !*silent {
			fmt.Fprintf(os.Stderr, "request failed: %v\n", reqErr)
		}
		os.Exit(1)
	}

	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	// =====================
	// response header 출력
	// =====================
	if *verboseFlag || *includeHeader {
		fmt.Fprintf(os.Stderr, "< %s\n", resp.Status)
		for k, v := range resp.Header {
			fmt.Fprintf(os.Stderr, "< %s: %s\n", k, v)
		}
		fmt.Fprintln(os.Stderr)
	}

	// =====================
	// output
	// =====================
	if *output != "" {
		if err := ioutil.WriteFile(*output, respBody, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error: write file: %v\n", err)
			os.Exit(1)
		}
	} else if !*silent {
		os.Stdout.Write(respBody)
	}

	// =====================
	// write-out
	// =====================
	if *writeOut != "" {
		out := *writeOut
		out = strings.ReplaceAll(out, "%{http_code}", strconv.Itoa(resp.StatusCode))
		out = strings.ReplaceAll(out, "%{time_total}", fmt.Sprintf("%.3f", duration))
		out = strings.ReplaceAll(out, "%{size_download}", strconv.Itoa(len(respBody)))
		out = strings.ReplaceAll(out, "%{url_effective}", rawURL)
		out = strings.ReplaceAll(out, "%{method}", strings.ToUpper(*method))
		fmt.Print(out)
	}
}
