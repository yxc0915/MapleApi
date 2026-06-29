package types

type SensitiveDetectionStatus string

const (
	SensitiveDetectionStatusAllowed   SensitiveDetectionStatus = "allowed"
	SensitiveDetectionStatusBlocked   SensitiveDetectionStatus = "blocked"
	SensitiveDetectionStatusBypassed  SensitiveDetectionStatus = "bypassed"
	SensitiveDetectionStatusErrorOpen SensitiveDetectionStatus = "error_open"
)

type SensitiveDetectionResult struct {
	Status         SensitiveDetectionStatus
	Checked        bool
	Trigger        string
	Objects        string
	Reason         string
	DetectorStatus int
}
