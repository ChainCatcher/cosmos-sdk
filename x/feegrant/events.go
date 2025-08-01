package feegrant

// feegrant module events
const (
	EventTypeUseFeeGrant    = "use_feegrant"
	EventTypeRevokeFeeGrant = "revoke_feegrant"
	EventTypeSetFeeGrant    = "set_feegrant"
	EventTypeUpdateFeeGrant = "update_feegrant"
	EventTypePruneFeeGrant  = "prune_feegrant"

	AttributeKeyGranter = "granter"
	AttributeKeyGrantee = "grantee"
	AttributeKeyPruner  = "pruner"
)
