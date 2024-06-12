package renderutils

import (
	"fmt"
	"strings"
)

type ConfigFile interface {
	Render() string
}

var (
	_ ConfigFile = &PropertiesConfig{}
	_ ConfigFile = &RawConfig{}
)

type PropertiesConfig struct {
	props []prop
}

type prop struct {
	key   string
	value any
}

func (c *PropertiesConfig) AddProperty(key string, value any) {
	c.props = append(c.props, prop{key: key, value: value})
}

func (c *PropertiesConfig) AddComment(comment string) {
	c.props = append(c.props, prop{key: "#", value: comment})
}

func (c *PropertiesConfig) Render() string {
	var res []string
	for _, p := range c.props {
		res = append(res, fmt.Sprintf("%s = %v", p.key, p.value))
	}
	return strings.Join(res, "\n")
}

type RawConfig struct {
	lines []string
}

func (c *RawConfig) AddLine(line string) {
	c.lines = append(c.lines, line)
}

func (c *RawConfig) Render() string {
	return strings.Join(c.lines, "\n")
}
