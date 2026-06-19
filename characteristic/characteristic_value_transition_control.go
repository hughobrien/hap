package characteristic

const TypeCharacteristicValueTransitionControl = "143"

type CharacteristicValueTransitionControl struct {
	*Bytes
}

func NewCharacteristicValueTransitionControl() *CharacteristicValueTransitionControl {
	c := NewBytes(TypeCharacteristicValueTransitionControl)
	c.Format = FormatTLV8
	c.Permissions = []string{PermissionRead, PermissionWrite, PermissionWriteResponse}
	c.Val = []byte{}

	return &CharacteristicValueTransitionControl{c}
}
