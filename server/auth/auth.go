package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Clave secreta para firmar los tokens (⚠️ En producción, almacénala en variables de entorno)
var jwtSecret = []byte("supersecreto")

// Generar un nuevo token JWT válido por 1 hora
func GenerateToken(username string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,
		"exp":      time.Now().Add(time.Hour).Unix(),
	})

	return token.SignedString(jwtSecret)
}

// Validar un token JWT
func ValidateToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verificar el método de firma
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return jwtSecret, nil
	})
}
