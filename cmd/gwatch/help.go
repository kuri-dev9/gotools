package main

const helpText = `
gwatch - show recent files from conf ini/json path patterns

Usage:
  gwatch [options] [project-or-conf-path]

Options:
  -w, --warn <minutes>   Color file names yellow when file-name time is old
                          enough compared to current time (default: 1)
  -c, --count <number>   Recent file count per path (default: 2)
  -i, --ignore <pattern> Ignore conf files or matched file names by glob
  -a, --all              Show file-name time and file size
  -h, --help             Show help
  -v, --version          Show version

Examples:
  gwatch
  gwatch /home/eva/PROJ
  gwatch /home/eva/PROJ/conf
  gwatch -c 1 -a
  gwatch -i recv*
`
