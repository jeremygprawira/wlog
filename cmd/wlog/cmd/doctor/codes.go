// This file lists the finding codes of wlog doctor, so wlog explain can name each one.
package doctor

// Codes returns every WLOG_DOCTOR_* code the doctor can report.
func Codes() []string {
	return []string{
		"WLOG_DOCTOR_LOAD",
		"WLOG_DOCTOR_MODULE",
		"WLOG_DOCTOR_MIDDLEWARE",
		"WLOG_DOCTOR_DRAINS",
		"WLOG_DOCTOR_ADAPTERS",
		"WLOG_DOCTOR_REDACTOR",
		"WLOG_DOCTOR_LOGGER",
		"WLOG_DOCTOR_SCORE",
	}
}
