module github.com/vibrantgio/cadence/popover

go 1.25.1

require (
	gioui.org v0.9.0
	github.com/reactivego/rx v0.2.2
	github.com/vibrantgio/prism/coordination v0.0.0
	github.com/vibrantgio/prism/theme v0.0.0
	github.com/vibrantgio/prism/tokens v0.0.0
)

require (
	gioui.org/shader v1.0.8 // indirect
	github.com/reactivego/scheduler v0.1.2 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

replace (
	github.com/vibrantgio/prism/coordination => ../../prism/coordination
	github.com/vibrantgio/prism/theme => ../../prism/theme
	github.com/vibrantgio/prism/tokens => ../../prism/tokens
)
