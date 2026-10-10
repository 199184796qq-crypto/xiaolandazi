package main

// The planner gets the angle to develop; the writer gets only the selected
// development direction. Do not resend an old angle's finished example prose
// as a second competing instruction to the writer.
func speechPacket(input map[string]any, selected, previous string) map[string]any {
	packet := map[string]any{}
	for k, v := range input {
		if k != "要推演的角度" {
			packet[k] = v
		}
	}
	packet["本段选中的讲述方向工作说明"] = selected
	packet["前段正文仅供理解和避免逐字重复"] = previous
	return packet
}
