package cli

import "fmt"

func PrintHelp(name string, content string) {
	fmt.Printf("Usage: %s [options] [args]\n\n", name)
	fmt.Println(content)
}

func PrintCustomHelp(contents ...string) {
	for _, c := range contents {
		fmt.Println(c)
	}
}
