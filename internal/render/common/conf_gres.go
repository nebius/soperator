package common

func GenerateGresConfig() ConfFile {
	res := &propertiesConfig{}
	res.addProperty("AutoDetect", "nvml")
	return res
}
