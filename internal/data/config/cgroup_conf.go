package config

func GenerateCGroupConfig() ConfFile {
	res := propertiesConfig{}
	res.addProperty("CgroupPlugin", "cgroup/v1")
	return res
}
