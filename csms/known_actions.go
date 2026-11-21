package csms

// The lists below are the schema file names of v16/schemas, v201/schemas and
// v21/schemas without their Request/Response suffix; TestOCPPActionsMatchSchemas
// fails when they drift.

// ocppActions lists the actions of each OCPP version, keyed by the version
// string of the schema registry. It bounds the "action" label of metrics.
var ocppActions = map[string]map[string]struct{}{
	"1.6": set(
		"Authorize", "BootNotification", "CancelReservation", "CertificateSigned",
		"ChangeAvailability", "ChangeConfiguration", "ClearCache", "ClearChargingProfile",
		"DataTransfer", "DeleteCertificate", "DiagnosticsStatusNotification", "ExtendedTriggerMessage",
		"FirmwareStatusNotification", "GetCompositeSchedule", "GetConfiguration", "GetDiagnostics",
		"GetInstalledCertificateIds", "GetLocalListVersion", "GetLog", "Heartbeat",
		"InstallCertificate", "LogStatusNotification", "MeterValues", "RemoteStartTransaction",
		"RemoteStopTransaction", "ReserveNow", "Reset", "SecurityEventNotification",
		"SendLocalList", "SetChargingProfile", "SignCertificate", "SignedFirmwareStatusNotification",
		"SignedUpdateFirmware", "StartTransaction", "StatusNotification", "StopTransaction",
		"TriggerMessage", "UnlockConnector", "UpdateFirmware",
	),
	"2.0.1": set(
		"Authorize", "BootNotification", "CancelReservation", "CertificateSigned",
		"ChangeAvailability", "ClearCache", "ClearChargingProfile", "ClearDisplayMessage",
		"ClearVariableMonitoring", "ClearedChargingLimit", "CostUpdated", "CustomerInformation",
		"DataTransfer", "DeleteCertificate", "FirmwareStatusNotification", "Get15118EVCertificate",
		"GetBaseReport", "GetCertificateStatus", "GetChargingProfiles", "GetCompositeSchedule",
		"GetDisplayMessages", "GetInstalledCertificateIds", "GetLocalListVersion", "GetLog",
		"GetMonitoringReport", "GetReport", "GetTransactionStatus", "GetVariables",
		"Heartbeat", "InstallCertificate", "LogStatusNotification", "MeterValues",
		"NotifyChargingLimit", "NotifyCustomerInformation", "NotifyDisplayMessages", "NotifyEVChargingNeeds",
		"NotifyEVChargingSchedule", "NotifyEvent", "NotifyMonitoringReport", "NotifyReport",
		"PublishFirmware", "PublishFirmwareStatusNotification", "ReportChargingProfiles", "RequestStartTransaction",
		"RequestStopTransaction", "ReservationStatusUpdate", "ReserveNow", "Reset",
		"SecurityEventNotification", "SendLocalList", "SetChargingProfile", "SetDisplayMessage",
		"SetMonitoringBase", "SetMonitoringLevel", "SetNetworkProfile", "SetVariableMonitoring",
		"SetVariables", "SignCertificate", "StatusNotification", "TransactionEvent",
		"TriggerMessage", "UnlockConnector", "UnpublishFirmware", "UpdateFirmware",
	),
	"2.1": set(
		"AFRRSignal", "AdjustPeriodicEventStream", "Authorize", "BatterySwap",
		"BootNotification", "CancelReservation", "CertificateSigned", "ChangeAvailability",
		"ChangeTransactionTariff", "ClearCache", "ClearChargingProfile", "ClearDERControl",
		"ClearDisplayMessage", "ClearTariffs", "ClearVariableMonitoring", "ClearedChargingLimit",
		"ClosePeriodicEventStream", "CostUpdated", "CustomerInformation", "DataTransfer",
		"DeleteCertificate", "FirmwareStatusNotification", "Get15118EVCertificate", "GetBaseReport",
		"GetCertificateChainStatus", "GetCertificateStatus", "GetChargingProfiles", "GetCompositeSchedule",
		"GetDERControl", "GetDisplayMessages", "GetInstalledCertificateIds", "GetLocalListVersion",
		"GetLog", "GetMonitoringReport", "GetPeriodicEventStream", "GetReport",
		"GetTariffs", "GetTransactionStatus", "GetVariables", "Heartbeat",
		"InstallCertificate", "LogStatusNotification", "MeterValues", "NotifyAllowedEnergyTransfer",
		"NotifyChargingLimit", "NotifyCustomerInformation", "NotifyDERAlarm", "NotifyDERStartStop",
		"NotifyDisplayMessages", "NotifyEVChargingNeeds", "NotifyEVChargingSchedule", "NotifyEvent",
		"NotifyMonitoringReport", "NotifyPeriodicEventStream", "NotifyPriorityCharging", "NotifyReport",
		"NotifySettlement", "NotifyWebPaymentStarted", "OpenPeriodicEventStream", "PublishFirmware",
		"PublishFirmwareStatusNotification", "PullDynamicScheduleUpdate", "ReportChargingProfiles", "ReportDERControl",
		"RequestBatterySwap", "RequestStartTransaction", "RequestStopTransaction", "ReservationStatusUpdate",
		"ReserveNow", "Reset", "SecurityEventNotification", "SendLocalList",
		"SetChargingProfile", "SetDERControl", "SetDefaultTariff", "SetDisplayMessage",
		"SetMonitoringBase", "SetMonitoringLevel", "SetNetworkProfile", "SetVariableMonitoring",
		"SetVariables", "SignCertificate", "StatusNotification", "TransactionEvent",
		"TriggerMessage", "UnlockConnector", "UnpublishFirmware", "UpdateDynamicSchedule",
		"UpdateFirmware", "UsePriorityCharging", "VatNumberValidation",
	),
}

func set(actions ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(actions))
	for _, a := range actions {
		m[a] = struct{}{}
	}
	return m
}
