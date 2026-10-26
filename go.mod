module github.com/nnutter/slush

go 1.27.1

tool (
	github.com/Antonboom/testifylint
	github.com/alecthomas/go-check-sumtype/cmd/go-check-sumtype
	github.com/kisielk/errcheck
	github.com/nnutter/constable/cmd/constable
	github.com/securego/gosec/v2/cmd/gosec
	github.com/zricethezav/gitleaks/v8
	go.uber.org/nilaway/cmd/nilaway
	golang.org/x/tools/cmd/goimports
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
	mvdan.cc/gofumpt
)

// gitleaks v8.30.1 pins mholt/archives v0.1.2, which does not compile
// against the rardecode version that constable pulls in. Raising the
// minimum makes Go select a compatible rardecode.
require github.com/mholt/archives v0.1.5 // indirect
