package domain

import "time"

// MixerCycle is one batch of an asphalt plant mixer, from the plant control
// system. Cycles are not signed one by one; they verify the lot's declared dosage.
type MixerCycle struct {
	TS         time.Time `json:"ts"`
	OrgID      string    `json:"orgId"`
	SiteID     string    `json:"siteId"`
	LotID      string    `json:"lotId"`
	Mix        string    `json:"mix"`
	Cycle      uint32    `json:"cycle"`
	BatchKg    float32   `json:"batchKg"`
	AdditiveKg float32   `json:"additiveKg"`
	BinderKg   float32   `json:"binderKg"`
	TC         float32   `json:"tempC"`
}

// Reading is one extruder line sample (melt temperature, pressure, throughput).
type Reading struct {
	TS            time.Time `json:"ts"`
	OrgID         string    `json:"orgId"`
	SiteID        string    `json:"siteId"`
	Line          string    `json:"line"`
	LotID         string    `json:"lotId,omitempty"`
	MeltC         float32   `json:"meltC"`
	PressureBar   float32   `json:"pressureBar"`
	ThroughputKgH float32   `json:"throughputKgH"`
	RPM           float32   `json:"rpm"`
	LoadPct       float32   `json:"loadPct"`
}
