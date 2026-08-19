module gtools

go 1.14

require (
	github.com/Shopify/sarama v1.29.1
	github.com/go-sql-driver/mysql v1.5.0
	github.com/jlaffaye/ftp v0.0.0-20210307004419-5d4190119067
	github.com/olekukonko/tablewriter v0.0.5
	github.com/pkg/sftp v1.12.0
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/pflag v1.0.5
	golang.org/x/crypto v0.0.0-20210616213533-5ff15b29337e
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1
)

// Preserve the versions used by the existing SSH/SFTP vendor tree.
replace golang.org/x/crypto => golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b

replace golang.org/x/sys => golang.org/x/sys v0.0.0-20201119102817-f84b799fce68
