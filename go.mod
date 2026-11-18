module us3

go 1.26.5

require (
	github.com/akalin/gopar v0.0.0-20210524065929-a776223763d3
	github.com/go-webgpu/goffi v0.6.3
	github.com/gogpu/systray v0.3.0
	github.com/hanwen/go-fuse/v2 v2.11.0
	golang.org/x/crypto v0.56.0
	golang.org/x/net v0.57.0
	golang.org/x/sys v0.47.0
)

require (
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/klauspost/cpuid/v2 v2.0.2 // indirect
)

// us3: retains the NSStatusItem across Show/Hide; see third_party/systray/US3-PATCH.md.
replace github.com/gogpu/systray => ./third_party/systray
