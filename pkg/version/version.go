package version

import (
	"fmt"
	"runtime"
)

var (
	// override to main
	Version = "0.0.1"

	// ldflags로 주입
	GitCommit = "none"
	BuildDate = "unknown"
)

func Info() string {
	return fmt.Sprintf(
		`Version   : %s
GitCommit : %s
BuildDate : %s
GoVersion : %s
OS/Arch   : %s/%s`,
		Version,
		GitCommit,
		BuildDate,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}

func Print() {
	fmt.Println(Info())
}
