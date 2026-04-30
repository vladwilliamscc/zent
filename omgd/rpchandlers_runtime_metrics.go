package main

import "omega/runtimemetrics"

func handleGetMinerRuntimeMetrics(s *rpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	m := runtimemetrics.Read()
	return map[string]uint64{
		"ip_minergap_reject_total":              m.IPMinerGapRejectTotal,
		"addr_minergap_reject_total":            m.AddrMinerGapRejectTotal,
		"template_build_success_total":          m.TemplateBuildSuccessTotal,
		"template_build_failure_total":          m.TemplateBuildFailureTotal,
		"miner_nonce_trials_total":              m.MinerNonceTrialsTotal,
		"committee_dial_failure_total":          m.CommitteeDialFailureTotal,
		"committee_participation_success_total": m.CommitteeParticipationSuccessTotal,
	}, nil
}
