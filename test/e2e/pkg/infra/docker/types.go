package docker

type DockerCompose struct {
	Version  string              `yaml:"version"`
	Networks map[string]Network  `yaml:"networks"`
	Services map[string]Service  `yaml:"services"`
}

type Network struct {
	Labels    map[string]bool `yaml:"labels"`
	Driver    string          `yaml:"driver"`
	EnableIPv6 bool           `yaml:"enable_ipv6,omitempty"`
	IPAM      IPAM            `yaml:"ipam"`
}

type IPAM struct {
	Driver string     `yaml:"driver"`
	Config []IPAMConfig `yaml:"config"`
}

type IPAMConfig struct {
	Subnet string `yaml:"subnet"`
}

type Service struct {
	Labels        map[string]bool `yaml:"labels"`
	ContainerName string          `yaml:"container_name"`
	Image         string          `yaml:"image"`
	Command       []string        `yaml:"command,omitempty"`
	Init          bool            `yaml:"init,omitempty"`
	Ports         []string        `yaml:"ports,omitempty"`
	Volumes       []string        `yaml:"volumes,omitempty"`
	Networks      map[string]NetworkConfig `yaml:"networks"`
}

type NetworkConfig struct {
	IPv4Address string `yaml:"ipv4_address,omitempty"`
	IPv6Address string `yaml:"ipv6_address,omitempty"`
}