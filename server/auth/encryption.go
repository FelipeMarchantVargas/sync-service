package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
)

// Ruta donde se almacenan las claves de los usuarios
const keyStorageDir = "./keys"

// Generar una clave AES-256 para un usuario
func GenerateAESKey(username string) (string, error) {
	key := make([]byte, 32) // 256 bits
	_, err := rand.Read(key)
	if err != nil {
		log.Printf("[ERROR] No se pudo generar clave AES para %s: %v", username, err)
		return "", err
	}

	// Guardar la clave en un archivo
	if _, err := os.Stat(keyStorageDir); os.IsNotExist(err) {
		os.Mkdir(keyStorageDir, os.ModePerm)
	}

	keyFile := keyStorageDir + "/" + username + ".key"
	err = os.WriteFile(keyFile, key, 0644)
	if err != nil {
		log.Printf("[ERROR] No se pudo escribir clave AES para %s: %v", username, err)
		return "", err
	}

	log.Printf("[SUCCESS] Clave AES generada para %s en %s", username, keyFile)
	return hex.EncodeToString(key), nil
}

// Obtener la clave AES-256 de un usuario
func GetAESKey(username string) ([]byte, error) {
	keyFile := keyStorageDir + "/" + username + ".key"

	// 📌 Verificar si la clave existe
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		log.Printf("[ERROR] Clave AES no encontrada para %s en %s", username, keyFile)
		return nil, errors.New("clave no encontrada, intenta iniciar sesión de nuevo")
	}

	key, err := os.ReadFile(keyFile)
	if err != nil {
		log.Printf("[ERROR] No se pudo leer la clave AES de %s: %v", username, err)
		return nil, errors.New("error al leer la clave de cifrado")
	}

	log.Printf("[SUCCESS] Clave AES cargada correctamente para %s", username)
	return key, nil
}


// Cifrar datos con AES-256
func EncryptData(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, aes.BlockSize+len(data))
	iv := ciphertext[:aes.BlockSize]
	_, err = io.ReadFull(rand.Reader, iv)
	if err != nil {
		return nil, err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], data)

	return ciphertext, nil
}

// Descifrar datos con AES-256
func DecryptData(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(data) < aes.BlockSize {
		return nil, errors.New("cifrado incorrecto")
	}

	iv := data[:aes.BlockSize]
	data = data[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(data, data)

	return data, nil
}
