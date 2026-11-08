package config

type Complexity struct {
	MaxCognitive        int `json:"max_cognitive"`
	MaxCyclomatic       int `json:"max_cyclomatic"`
	MaxNesting          int `json:"max_nesting"`
	MaxLines            int `json:"max_lines"`
	MaxParams           int `json:"max_params"`
	MaxBoolParams       int `json:"max_bool_params"`
	MaxInterfaceMethods int `json:"max_interface_methods"`
	MaxFileLines        int `json:"max_file_lines"`
}

var complexityByMode = [...]Complexity{
	Basic: {
		MaxCognitive:        24,
		MaxCyclomatic:       30,
		MaxNesting:          6,
		MaxLines:            150,
		MaxParams:           8,
		MaxBoolParams:       0,
		MaxInterfaceMethods: 0,
		MaxFileLines:        1499,
	},
	Standard: {
		MaxCognitive:        19,
		MaxCyclomatic:       15,
		MaxNesting:          5,
		MaxLines:            80,
		MaxParams:           6,
		MaxBoolParams:       2,
		MaxInterfaceMethods: 10,
		MaxFileLines:        999,
	},
	Strict: {
		MaxCognitive:        14,
		MaxCyclomatic:       9,
		MaxNesting:          3,
		MaxLines:            60,
		MaxParams:           4,
		MaxBoolParams:       1,
		MaxInterfaceMethods: 5,
		MaxFileLines:        799,
	},
}

func ComplexityFor(m Mode) Complexity {
	return complexityByMode[m]
}
