package usersnapshot

type GetInput struct {
	UID           uint64 `json:"uid" jsonschema:"PSL user ID"`
	BackpackLimit int    `json:"backpack_limit,omitempty" jsonschema:"maximum rows per backpack category; 1 to 100"`
}

type Section struct {
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated,omitempty"`
	Error     string           `json:"error,omitempty"`
}

type Backpack struct {
	Commodities Section `json:"commodities"`
	Equipped    Section `json:"equipped"`
	PropCards   Section `json:"prop_cards"`
}

type GetOutput struct {
	UID      uint64   `json:"uid"`
	User     Section  `json:"user"`
	VIP      Section  `json:"vip"`
	Backpack Backpack `json:"backpack"`
}
