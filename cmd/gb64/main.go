package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

const versionInfo = "1.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(helpText)
		return nil
	}
	switch args[0] {
	case "version", "-V", "--version":
		version.Version = versionInfo
		version.Print()
		return nil
	case "encode":
		return runEncode(args[1:])
	case "decode":
		return runDecode(args[1:])
	case "drop":
		return runDrop(args[1:])
	default:
		return runDrop(args)
	}
}

func flagsFor(command, help string) (*pflag.FlagSet, *bool) {
	flags := pflag.NewFlagSet("gb64 "+command, pflag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	helpFlag := flags.BoolP("help", "h", false, "show help")
	flags.Usage = func() { fmt.Print(help) }
	return flags, helpFlag
}

func oneInput(flags *pflag.FlagSet) (string, error) {
	if flags.NArg() > 1 {
		return "", fmt.Errorf("too many input files")
	}
	if flags.NArg() == 1 {
		return flags.Arg(0), nil
	}
	return "", nil
}

func input(path string) (io.Reader, func() error, error) {
	if path == "" || path == "-" {
		return os.Stdin, func() error { return nil }, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open input: %v", err)
	}
	return f, f.Close, nil
}

func runEncode(args []string) error {
	flags, help := flagsFor("encode", encodeHelp)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *help {
		flags.Usage()
		return nil
	}
	path, err := oneInput(flags)
	if err != nil {
		return err
	}
	r, closeInput, err := input(path)
	if err != nil {
		return err
	}
	defer closeInput()
	encoder := base64.NewEncoder(base64.StdEncoding, os.Stdout)
	if _, err = io.Copy(encoder, r); err != nil {
		return err
	}
	if err = encoder.Close(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout)
	return err
}

type whitespaceReader struct{ r *bufio.Reader }

func (r *whitespaceReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		c, err := r.r.ReadByte()
		if err != nil {
			if n > 0 && err == io.EOF {
				return n, nil
			}
			return n, err
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		p[n] = c
		n++
	}
	return n, nil
}

func runDecode(args []string) error {
	flags, help := flagsFor("decode", decodeHelp)
	output := flags.StringP("output", "o", "", "output file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *help {
		flags.Usage()
		return nil
	}
	path, err := oneInput(flags)
	if err != nil {
		return err
	}
	r, closeInput, err := input(path)
	if err != nil {
		return err
	}
	defer closeInput()
	var writer io.Writer = os.Stdout
	closeOutput := func() error { return nil }
	if *output != "" {
		f, createErr := os.Create(*output)
		if createErr != nil {
			return fmt.Errorf("create output: %v", createErr)
		}
		writer, closeOutput = f, f.Close
	}
	decoder := base64.NewDecoder(base64.StdEncoding, &whitespaceReader{r: bufio.NewReader(r)})
	if _, err = io.Copy(writer, decoder); err != nil {
		closeOutput()
		return fmt.Errorf("decode: %v", err)
	}
	return closeOutput()
}
