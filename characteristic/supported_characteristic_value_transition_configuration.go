package characteristic

const TypeSupportedCharacteristicValueTransitionConfiguration = "144"

type SupportedCharacteristicValueTransitionConfiguration struct {
	*Bytes
}

func NewSupportedCharacteristicValueTransitionConfiguration() *SupportedCharacteristicValueTransitionConfiguration {
	c := NewBytes(TypeSupportedCharacteristicValueTransitionConfiguration)
	c.Format = FormatTLV8
	c.Permissions = []string{PermissionRead}
	c.Val = []byte{}

	return &SupportedCharacteristicValueTransitionConfiguration{c}
}
