package model

type DeviceAddressingPreference struct {
	Mode string `json:"mode"`
}

func ValidSpeakerAddressing(value string) bool {
	return value == "female" || value == "male" || value == "child" || value == "neutral"
}
func ValidDeviceAddressingMode(value string) bool {
	return value == "auto" || ValidSpeakerAddressing(value)
}

type DeviceVoiceAddressing struct {
	Addressing string
	Source     string
}
