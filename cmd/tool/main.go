package main

import (
	"fmt"

	_ "github.com/go-sql-driver/mysql"

	"github.com/olekukonko/tablewriter"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

func main() {
	// logrus 사용
	logrus.Info("init")

	// pflag 사용
	var test string
	pflag.StringVar(&test, "test", "ok", "test flag")
	pflag.Parse()

	// tablewriter 사용
	table := tablewriter.NewWriter(nil)
	table.SetHeader([]string{"A", "B"})

	fmt.Println("vendor seed:", test)
}