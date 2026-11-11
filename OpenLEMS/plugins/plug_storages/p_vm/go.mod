module p_vm

go 1.25

require (
	common v0.0.0
	github.com/pkg/errors v0.9.1
	github.com/shockerli/cvt v0.2.8
)

require (
	github.com/kr/text v0.2.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace common => ../../../common
