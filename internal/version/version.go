package version

const Current = "v1.1.4"

var Override = ""

func String() string {
	if Override != "" {
		return Override
	}
	return Current
}
