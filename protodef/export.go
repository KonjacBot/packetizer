package protodef

func SanitizeTypeNameForCLI(name string) string {
	return sanitizeTypeName(name)
}
