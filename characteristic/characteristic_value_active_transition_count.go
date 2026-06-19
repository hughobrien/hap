package characteristic

const TypeCharacteristicValueActiveTransitionCount = "24B"

type CharacteristicValueActiveTransitionCount struct {
	*Int
}

func NewCharacteristicValueActiveTransitionCount() *CharacteristicValueActiveTransitionCount {
	c := NewInt(TypeCharacteristicValueActiveTransitionCount)
	c.Format = FormatUInt8
	c.Permissions = []string{PermissionRead, PermissionEvents}
	c.SetValue(0)

	return &CharacteristicValueActiveTransitionCount{c}
}
