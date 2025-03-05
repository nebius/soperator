package renderutils

import (
	"fmt"
	"strings"
)

const (
	DefaultConnector   = EqualSignConnector
	EqualSignConnector = "="
	SpaceConnector     = " "
)

type ConfigFile interface {
	Render() string
}

var (
	_ ConfigFile = &PropertiesConfig{}
	_ ConfigFile = &MultilineStringConfig{}
	_ ConfigFile = &AsIsConfig{}
)

type PropertiesConfig struct {
	props []prop
}

type prop struct {
	key       string
	value     any
	connector string
}

func (c *PropertiesConfig) AddProperty(key string, value any) {
	c.props = append(c.props, prop{key: key, value: value, connector: DefaultConnector})
}

func (c *PropertiesConfig) AddPropertyWithConnector(key string, value any, connector string) {
	c.props = append(c.props, prop{key: key, value: value, connector: connector})
}

func (c *PropertiesConfig) AddComment(comment string) {
	c.props = append(c.props, prop{key: "#", value: comment})
}

func (c *PropertiesConfig) Render() string {
	var res []string
	for _, p := range c.props {
		res = append(res, fmt.Sprintf("%s%s%v", p.key, p.connector, p.value))
	}
	return strings.Join(res, "\n")
}

type MultilineStringConfig struct {
	lines []string
}

func (c *MultilineStringConfig) AddLine(line string) {
	c.lines = append(c.lines, line)
}

func (c *MultilineStringConfig) Render() string {
	return strings.Join(c.lines, "\n")
}

type AsIsConfig struct {
	config string
}

func (c AsIsConfig) Render() string {
	return c.config
}

func NewAsIsConfig(config string) ConfigFile {
	return AsIsConfig{config: config}
}
