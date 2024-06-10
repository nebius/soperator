package common

import (
	"fmt"
	"strings"
)

type ConfFile interface {
	Render() string
}

var (
	_ ConfFile = &propertiesConfig{}
	_ ConfFile = &rawConfig{}
)

type propertiesConfig struct {
	props []prop
}

type prop struct {
	key   string
	value any
}

func (c *propertiesConfig) addProperty(key string, value any) {
	c.props = append(c.props, prop{key: key, value: value})
}

func (c *propertiesConfig) addComment(comment string) {
	c.props = append(c.props, prop{key: "#", value: comment})
}

func (c *propertiesConfig) Render() string {
	var res []string
	for _, p := range c.props {
		res = append(res, fmt.Sprintf("%s = %v", p.key, p.value))
	}
	return strings.Join(res, "\n")
}

type rawConfig struct {
	lines []string
}

func (c *rawConfig) addLine(line string) {
	c.lines = append(c.lines, line)
}

func (c *rawConfig) Render() string {
	return strings.Join(c.lines, "\n")
}
