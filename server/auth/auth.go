package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Clave secreta (⚠️ Mueve esto a una variable de entorno en producción)
var jwtSecret = []byte("supersecreto")

// Generar un nuevo token JWT válido por 1 hora
func GenerateToken(username string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,
		"exp":      time.Now().Add(time.Hour).Unix(), // Expira en 1 hora
	})

	return token.SignedString(jwtSecret)
}

// Generar un Refresh Token válido por 7 días
func GenerateRefreshToken(username string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,
		"exp":      time.Now().Add(7 * 24 * time.Hour).Unix(), // Expira en 7 días
	})

	return token.SignedString(jwtSecret)
}


// Validar un token JWT y verificar expiración
func ValidateToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	// Verificar que el token es válido y no ha expirado
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		exp, ok := claims["exp"].(float64)
		if !ok {
			return nil, errors.New("token sin fecha de expiración")
		}
		if time.Now().Unix() > int64(exp) {
			return nil, errors.New("token expirado")
		}
		return token, nil
	}

	return nil, errors.New("token inválido")
}
