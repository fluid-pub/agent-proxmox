module fluid/agents/proxmox

go 1.23

require (
	fluid/agents/core v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/gorilla/websocket v1.5.3 // indirect

replace fluid/agents/core => ./core
