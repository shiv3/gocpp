package ocpp2_test

import (
	"fmt"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/types"
)

// Test
func (suite *OcppV2TestSuite) TestGetCompositeScheduleRequestValidation() {
	t := suite.T()
	var requestTable = []GenericTestEntry{
		{smartcharging.GetCompositeScheduleRequest{Duration: 600, EvseID: 1, ChargingRateUnit: types.ChargingRateUnitWatts}, true},
		{smartcharging.GetCompositeScheduleRequest{Duration: 600, EvseID: 1}, true},
		{smartcharging.GetCompositeScheduleRequest{EvseID: 1}, true},
		{smartcharging.GetCompositeScheduleRequest{}, true},
		{smartcharging.GetCompositeScheduleRequest{Duration: 600, EvseID: -1, ChargingRateUnit: types.ChargingRateUnitWatts}, false},
		{smartcharging.GetCompositeScheduleRequest{Duration: -1, EvseID: 1, ChargingRateUnit: types.ChargingRateUnitWatts}, false},
		{smartcharging.GetCompositeScheduleRequest{Duration: 600, EvseID: 1, ChargingRateUnit: "invalidChargingRateUnit"}, false},
	}
	ExecuteGenericTestTable(t, requestTable)
}

func (suite *OcppV2TestSuite) TestGetCompositeScheduleConfirmationValidation() {
	t := suite.T()
	period := types.NewChargingSchedulePeriod(0, 10.0)
	period.NumberPhases = newInt(1)
	period.PhaseToUse = newInt(2)
	compositeSchedule := smartcharging.CompositeSchedule{
		ChargingSchedulePeriod: []types.ChargingSchedulePeriod{period},
		EvseID:                 1,
		Duration:               600,
		ScheduleStart:          types.NewDateTime(time.Now()),
		ChargingRateUnit:       types.ChargingRateUnitWatts,
	}
	invalidUnit := compositeSchedule
	invalidUnit.ChargingRateUnit = "invalidChargingRateUnit"
	noPeriods := compositeSchedule
	noPeriods.ChargingSchedulePeriod = nil
	noStart := compositeSchedule
	noStart.ScheduleStart = nil
	negativeDuration := compositeSchedule
	negativeDuration.Duration = -1
	var confirmationTable = []GenericTestEntry{
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted, Schedule: &compositeSchedule}, true},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusRejected, StatusInfo: types.NewStatusInfo("noProfile", "")}, true},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted}, true},
		{smartcharging.GetCompositeScheduleResponse{}, false},
		{smartcharging.GetCompositeScheduleResponse{Status: "invalidGetCompositeScheduleStatus"}, false},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted, Schedule: &smartcharging.CompositeSchedule{}}, false},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted, Schedule: &invalidUnit}, false},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted, Schedule: &noPeriods}, false},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted, Schedule: &noStart}, false},
		{smartcharging.GetCompositeScheduleResponse{Status: smartcharging.GetCompositeScheduleStatusAccepted, Schedule: &negativeDuration}, false},
	}
	ExecuteGenericTestTable(t, confirmationTable)
}

func (suite *OcppV2TestSuite) TestGetCompositeScheduleE2EMocked() {
	t := suite.T()
	wsId := "test_id"
	messageId := defaultMessageId
	wsUrl := "someUrl"
	evseID := 1
	chargingRateUnit := types.ChargingRateUnitWatts
	duration := 600
	status := smartcharging.GetCompositeScheduleStatusAccepted
	scheduleStart := types.NewDateTime(time.Now())
	chargingSchedulePeriod := types.NewChargingSchedulePeriod(0, 10.0)
	chargingSchedulePeriod.NumberPhases = newInt(1)
	chargingSchedulePeriod.PhaseToUse = newInt(2)
	compositeSchedule := smartcharging.CompositeSchedule{
		ChargingSchedulePeriod: []types.ChargingSchedulePeriod{chargingSchedulePeriod},
		EvseID:                 evseID,
		Duration:               duration,
		ScheduleStart:          scheduleStart,
		ChargingRateUnit:       chargingRateUnit,
	}
	requestJson := fmt.Sprintf(`[2,"%v","%v",{"duration":%v,"chargingRateUnit":"%v","evseId":%v}]`,
		messageId, smartcharging.GetCompositeScheduleFeatureName, duration, chargingRateUnit, evseID)
	responseJson := fmt.Sprintf(`[3,"%v",{"status":"%v","schedule":{"chargingSchedulePeriod":[{"startPeriod":%v,"limit":%v,"numberPhases":%v,"phaseToUse":%v}],"evseId":%v,"duration":%v,"scheduleStart":"%v","chargingRateUnit":"%v"}}]`,
		messageId, status, chargingSchedulePeriod.StartPeriod, chargingSchedulePeriod.Limit, *chargingSchedulePeriod.NumberPhases, *chargingSchedulePeriod.PhaseToUse, evseID, duration, scheduleStart.FormatTimestamp(), chargingRateUnit)
	getCompositeScheduleConfirmation := smartcharging.NewGetCompositeScheduleResponse(status)
	getCompositeScheduleConfirmation.Schedule = &compositeSchedule
	channel := NewMockWebSocket(wsId)

	handler := &MockChargingStationSmartChargingHandler{}
	handler.On("OnGetCompositeSchedule", mock.Anything).Return(getCompositeScheduleConfirmation, nil).Run(func(args mock.Arguments) {
		request, ok := args.Get(0).(*smartcharging.GetCompositeScheduleRequest)
		assert.True(t, ok)
		assert.NotNil(t, request)
		assert.Equal(t, duration, request.Duration)
		assert.Equal(t, chargingRateUnit, request.ChargingRateUnit)
		assert.Equal(t, evseID, request.EvseID)
	})
	setupDefaultCSMSHandlers(suite, expectedCSMSOptions{clientId: wsId, rawWrittenMessage: []byte(requestJson), forwardWrittenMessage: true})
	setupDefaultChargingStationHandlers(suite, expectedChargingStationOptions{serverUrl: wsUrl, clientId: wsId, createChannelOnStart: true, channel: channel, rawWrittenMessage: []byte(responseJson), forwardWrittenMessage: true}, handler)
	// Run Test
	suite.csms.Start(8887, "somePath")
	err := suite.chargingStation.Start(wsUrl)
	assert.Nil(t, err)
	resultChannel := make(chan bool, 1)
	err = suite.csms.GetCompositeSchedule(wsId, func(confirmation *smartcharging.GetCompositeScheduleResponse, err error) {
		require.Nil(t, err)
		require.NotNil(t, confirmation)
		assert.Equal(t, status, confirmation.Status)
		require.NotNil(t, confirmation.Schedule)
		assert.Equal(t, evseID, confirmation.Schedule.EvseID)
		assert.Equal(t, duration, confirmation.Schedule.Duration)
		assert.Equal(t, chargingRateUnit, confirmation.Schedule.ChargingRateUnit)
		require.NotNil(t, confirmation.Schedule.ScheduleStart)
		assert.Equal(t, scheduleStart.FormatTimestamp(), confirmation.Schedule.ScheduleStart.FormatTimestamp())
		require.Len(t, confirmation.Schedule.ChargingSchedulePeriod, 1)
		assert.Equal(t, chargingSchedulePeriod.Limit, confirmation.Schedule.ChargingSchedulePeriod[0].Limit)
		assert.Equal(t, chargingSchedulePeriod.StartPeriod, confirmation.Schedule.ChargingSchedulePeriod[0].StartPeriod)
		require.NotNil(t, confirmation.Schedule.ChargingSchedulePeriod[0].NumberPhases)
		assert.Equal(t, *chargingSchedulePeriod.NumberPhases, *confirmation.Schedule.ChargingSchedulePeriod[0].NumberPhases)
		require.NotNil(t, confirmation.Schedule.ChargingSchedulePeriod[0].PhaseToUse)
		assert.Equal(t, *chargingSchedulePeriod.PhaseToUse, *confirmation.Schedule.ChargingSchedulePeriod[0].PhaseToUse)
		resultChannel <- true
	}, duration, evseID, func(request *smartcharging.GetCompositeScheduleRequest) {
		request.ChargingRateUnit = chargingRateUnit
	})
	assert.Nil(t, err)
	result := <-resultChannel
	assert.True(t, result)
}

func (suite *OcppV2TestSuite) TestGetCompositeScheduleInvalidEndpoint() {
	messageId := defaultMessageId
	evseID := 1
	chargingRateUnit := types.ChargingRateUnitWatts
	duration := 600
	requestJson := fmt.Sprintf(`[2,"%v","%v",{"duration":%v,"chargingRateUnit":"%v","evseId":%v}]`,
		messageId, smartcharging.GetCompositeScheduleFeatureName, duration, chargingRateUnit, evseID)
	getCompositeScheduleRequest := smartcharging.NewGetCompositeScheduleRequest(evseID, duration)
	getCompositeScheduleRequest.ChargingRateUnit = chargingRateUnit
	testUnsupportedRequestFromChargingStation(suite, getCompositeScheduleRequest, requestJson, messageId)
}
