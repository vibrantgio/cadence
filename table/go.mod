module github.com/vibrantgio/cadence/table

go 1.25.1

require (
	gioui.org v0.9.0
	github.com/reactivego/rx v0.2.2
	github.com/vibrantgio/prism/list v0.0.0
	github.com/vibrantgio/prism/theme v0.0.0
	github.com/vibrantgio/prism/tokens v0.0.0
)

require (
	github.com/go-text/typesetting v0.3.0 // indirect
	github.com/reactivego/scheduler v0.1.2 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/image v0.26.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

replace (
	github.com/vibrantgio/prism/internal/golden => ../../prism/internal/golden
	github.com/vibrantgio/prism/list => ../../prism/list
	github.com/vibrantgio/prism/theme => ../../prism/theme
	github.com/vibrantgio/prism/tokens => ../../prism/tokens
)
