package database

// Keep nullable UUIDs consistent with the string representation used for
// non-null UUIDs throughout the generated database API.
var (
	_ *string = AuditEvent{}.OperationID
	_ *string = AuditEvent{}.IntentEventID
	_ *string = AuditEvent{}.ActorIdentityID
	_ *string = AuditEvent{}.ActorTokenID
	_ *string = Delegation{}.GranteeIdentityID
	_ *string = Delegation{}.GranteeGroupID
	_ *string = Delegation{}.RevokedByIdentityID
	_ *string = ZoneBinding{}.RetiredByIdentityID

	_ *string = GetAuditEventByIDRow{}.OperationID
	_ *string = GetAuditEventByIDRow{}.IntentEventID
	_ *string = GetAuditEventByIDRow{}.ActorIdentityID
	_ *string = GetAuditEventByIDRow{}.ActorTokenID
	_ *string = InsertAuthorizationDeniedAuditRow{}.OperationID
	_ *string = InsertAuthorizationDeniedAuditRow{}.IntentEventID
	_ *string = InsertAuthorizationDeniedAuditRow{}.ActorIdentityID
	_ *string = InsertAuthorizationDeniedAuditRow{}.ActorTokenID
	_ *string = InsertDNSIntentRow{}.OperationID
	_ *string = InsertDNSIntentRow{}.IntentEventID
	_ *string = InsertDNSIntentRow{}.ActorIdentityID
	_ *string = InsertDNSIntentRow{}.ActorTokenID
	_ *string = InsertManagementAuditRow{}.OperationID
	_ *string = InsertManagementAuditRow{}.IntentEventID
	_ *string = InsertManagementAuditRow{}.ActorIdentityID
	_ *string = InsertManagementAuditRow{}.ActorTokenID
)
