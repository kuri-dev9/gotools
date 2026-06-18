package main

const helpText = `
Options:
  -L, --level <n>        Max depth level
  -a, --all              Show hidden files
  -d, --dir              Directories only
  -f, --full             Show full path
  -s, --size             Show file size
      --human            Human readable size
      --sort <name|size> Sort entries
      --json             Output JSON
      --summary          Show summary

  -h, --help             Show help
  -v, --version          Show version

Examples:
  gtree
  gtree /var/log
  gtree -L 2
  gtree -s -h --sort size
  gtree --json | jq
  `
