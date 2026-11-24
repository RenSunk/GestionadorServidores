package projects

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gestionador/internal/sshx"
)

// EnvVar es una variable de entorno del proyecto. Todas llegan al contenedor;
// las marcadas con Build además se pasan como --build-arg al construir la imagen.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Build bool   `json:"build"`
}

const (
	maxEnvVars     = 200
	maxEnvValueLen = 32 << 10
)

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedEnv las define Gestionador (PORT) o alterarían al cliente docker en el
// servidor, porque los valores pasan por su entorno al crear el contenedor.
var reservedEnv = []string{"PORT", "PATH", "HOME", "IFS", "ENV", "BASH_ENV", "LD_PRELOAD", "LD_LIBRARY_PATH"}

// normalizeEnv valida las variables y las ordena por nombre.
func normalizeEnv(vars []EnvVar) ([]EnvVar, error) {
	if len(vars) > maxEnvVars {
		return nil, invalid("máximo %d variables de entorno", maxEnvVars)
	}
	seen := map[string]bool{}
	out := make([]EnvVar, 0, len(vars))
	for _, v := range vars {
		v.Key = strings.TrimSpace(v.Key)
		switch {
		case v.Key == "" && v.Value == "":
			continue // fila vacía del formulario
		case !envKeyRe.MatchString(v.Key):
			return nil, invalid("nombre de variable inválido %q: usa letras, números y _ (sin empezar por número)", v.Key)
		case strings.ToUpper(v.Key) == "PORT":
			return nil, invalid("PORT la define Gestionador a partir del puerto del contenedor")
		case slices.Contains(reservedEnv, strings.ToUpper(v.Key)) || strings.HasPrefix(strings.ToUpper(v.Key), "DOCKER_"):
			return nil, invalid("%s es un nombre reservado (lo usa el sistema del servidor al crear el contenedor)", v.Key)
		case seen[v.Key]:
			return nil, invalid("la variable %s está repetida", v.Key)
		case strings.ContainsRune(v.Value, 0):
			return nil, invalid("el valor de %s tiene caracteres no válidos", v.Key)
		case len(v.Value) > maxEnvValueLen:
			return nil, invalid("el valor de %s es demasiado largo", v.Key)
		}
		seen[v.Key] = true
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b EnvVar) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

// buildVars devuelve solo las variables disponibles durante el build.
func buildVars(vars []EnvVar) []EnvVar {
	var out []EnvVar
	for _, v := range vars {
		if v.Build {
			out = append(out, v)
		}
	}
	return out
}

func envKeys(vars []EnvVar) []string {
	keys := make([]string, len(vars))
	for i, v := range vars {
		keys[i] = v.Key
	}
	return keys
}

// envScript genera el script que se carga con `. /dev/stdin` antes de `docker run`.
// Los valores van entre comillas simples escapadas: no se expande nada.
func envScript(vars []EnvVar) string {
	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "export %s=%s\n", v.Key, sshx.Quote(v.Value))
	}
	return b.String()
}

func (s *Service) decryptEnv(p Project) ([]EnvVar, error) {
	if p.EnvEnc == "" {
		return []EnvVar{}, nil
	}
	raw, err := s.box.Decrypt(p.EnvEnc)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron descifrar las variables de entorno: %w", err)
	}
	var vars []EnvVar
	if err := json.Unmarshal(raw, &vars); err != nil {
		return nil, err
	}
	return vars, nil
}

func (s *Service) encryptEnv(vars []EnvVar) (string, error) {
	if len(vars) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(vars)
	if err != nil {
		return "", err
	}
	return s.box.Encrypt(raw)
}
