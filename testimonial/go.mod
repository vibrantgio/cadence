module github.com/vibrantgio/cadence/testimonial

go 1.25.1

require (
	gioui.org v0.9.0
	github.com/reactivego/rx v0.2.2
	github.com/vibrantgio/prism/layout v0.0.0
	github.com/vibrantgio/prism/theme v0.0.0
	github.com/vibrantgio/prism/tokens v0.0.0
)

require (
	gioui.org/shader v1.0.8 // indirect
	github.com/go-text/typesetting v0.3.0 // indirect
	github.com/reactivego/scheduler v0.1.2 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/image v0.26.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

replace (
	github.com/vibrantgio/prism/layout => ../../prism/layout
	github.com/vibrantgio/prism/theme => ../../prism/theme
	github.com/vibrantgio/prism/tokens => ../../prism/tokens
)
