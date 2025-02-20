package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"github.com/FelipeMarchantVargas/sync-service/server/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Estructura para almacenar credenciales
type Credentials struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// Configurar logrus con archivo de logs
var log = logrus.New()

func init() {
	file, err := os.OpenFile("client.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("No se pudo abrir el archivo de logs: %v", err)
	}
	log.Out = file
	log.SetFormatter(&logrus.JSONFormatter{}) // Logs en JSON
}

// -------------------- AUTENTICACIÓN ---------------------------

// Guardar credenciales en un archivo JSON
func saveCredentials(creds Credentials) error {
	file, err := os.Create("credentials.json")
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(creds)
}

// Cargar credenciales desde un archivo JSON
func loadCredentials() (Credentials, error) {
	var creds Credentials
	file, err := os.Open("credentials.json")
	if err != nil {
		return creds, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&creds)
	return creds, err
}

// Refrescar token si es necesario
func refreshAuthToken(authClient pb.AuthServiceClient, refreshToken string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := authClient.RefreshToken(ctx, &pb.RefreshRequest{RefreshToken: refreshToken})
	if err != nil {
		return "", "", err
	}

	// Guardar nuevos tokens
	saveCredentials(Credentials{Token: resp.Token, RefreshToken: resp.RefreshToken})
	return resp.Token, resp.RefreshToken, nil
}

// Iniciar sesión o usar tokens guardados
func authenticate(authClient pb.AuthServiceClient) (string, string, error) {
	creds, err := loadCredentials()
	if err == nil {
		return creds.Token, creds.RefreshToken, nil
	}

	// Si no hay credenciales almacenadas, iniciar sesión
	token, refreshToken, err := login(authClient, "admin", "password")
	if err != nil {
		return "", "", err
	}
	saveCredentials(Credentials{Token: token, RefreshToken: refreshToken})
	return token, refreshToken, nil
}

// Obtener contexto con autenticación
func getAuthContext(authClient pb.AuthServiceClient) (context.Context, error) {
	token, refreshToken, err := authenticate(authClient)
	if err != nil {
		return nil, err
	}

	// Intentar renovar el token si ha expirado
	newToken, newRefreshToken, err := refreshAuthToken(authClient, refreshToken)
	if err == nil {
		token = newToken
		refreshToken = newRefreshToken
		saveCredentials(Credentials{Token: token, RefreshToken: refreshToken})
	}

	// Crear contexto con token en metadata
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", token))
	return ctx, nil
}

// ------------------------- SINCRONIZACIÓN --------------------------------

func uploadFile(client pb.SyncServiceClient, filePath string, ctx context.Context) error {
	startTime := time.Now()

	// Extraer solo el nombre base del archivo
	filename := filepath.Base(filePath)

	log.WithFields(logrus.Fields{
		"file": filename,
	}).Info("Iniciando subida de archivo")

	// Obtener tamaño del archivo
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo obtener info del archivo: %v", err)
	}
	fileSize := fileInfo.Size()

	log.Printf("[INFO] (%s) Subiendo %s (%d KB)", time.Now().Format("15:04:05"), filename, fileSize/1024)

	// Abrir el archivo original
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo abrir el archivo: %v", err)
	}
	defer file.Close()

	// Comprimir el archivo en memoria
	var compressedBuffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuffer)

	_, err = io.Copy(gzipWriter, file)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo comprimir el archivo: %v", err)
	}
	gzipWriter.Close()

	// Crear stream para enviar el archivo comprimido
	stream, err := client.UploadFile(ctx)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo iniciar la subida: %v", err)
	}

	chunkSize := 1024
	compressedData := compressedBuffer.Bytes()
	for i := 0; i < len(compressedData); i += chunkSize {
		end := i + chunkSize
		if end > len(compressedData) {
			end = len(compressedData)
		}

		err := stream.Send(&pb.FileChunk{
			Filename: filename, // 🔥 No agregamos ".gz"
			Data:     compressedData[i:end],
		})
		if err != nil {
			log.Fatalf("[ERROR] No se pudo enviar el fragmento: %v", err)
		}
	}

	// Cerrar transmisión
	_, err = stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("[ERROR] Error al cerrar la subida: %v", err)
	}
	elapsed := time.Since(startTime)
	log.Printf("[SUCCESS] (%s) %s subido en %.2f s", time.Now().Format("15:04:05"), filename, elapsed.Seconds())
	return nil
}

func downloadFile(client pb.SyncServiceClient, filename string, ctx context.Context) error {
	startTime := time.Now()
	log.Printf("[INFO] (%s) Descargando %s...", time.Now().Format("15:04:05"), filename)

	stream, err := client.DownloadFile(ctx, &pb.FileRequest{Filename: filename})
	if err != nil {
		log.Fatalf("[ERROR] No se pudo solicitar el archivo: %v", err)
	}

	var compressedBuffer bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("[ERROR] No se pudo recibir el fragmento: %v", err)
		}
		compressedBuffer.Write(chunk.Data)
	}

	// Descomprimir archivo
	reader, err := gzip.NewReader(&compressedBuffer)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo descomprimir el archivo: %v", err)
	}

	decompressedData, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		log.Fatalf("[ERROR] Error al leer archivo descomprimido: %v", err)
	}

	// 🔥 Si el archivo tiene `.gz`, eliminarlo del nombre antes de guardar
	cleanFilename := filename
	if filepath.Ext(filename) == ".gz" {
		cleanFilename = filename[:len(filename)-3]
	}

	// Guardar el archivo descomprimido con su nombre correcto
	filePath := filepath.Join("./", cleanFilename)
	err = os.WriteFile(filePath, decompressedData, 0644)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo guardar el archivo: %v", err)
	}

	elapsed := time.Since(startTime)
	log.Printf("[SUCCESS] (%s) %s descargado en %.2f s", time.Now().Format("15:04:05"), cleanFilename, elapsed.Seconds())
	return nil
}

func listFiles(client pb.SyncServiceClient, ctx context.Context) error {
	startTime := time.Now()
	log.Println("[INFO] Solicitando lista de archivos...")

	resp, err := client.ListFiles(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("[ERROR] No se pudo obtener la lista de archivos: %v", err)
		return err
	}

	if len(resp.Filenames) == 0 {
		log.Println("[INFO] No hay archivos en el servidor.")
		return nil
	}

	// Obtener el nombre de usuario desde el contexto
	username, err := getUsernameFromContext(ctx)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo obtener el nombre de usuario: %v", err)
		return err
	}

	// Crear el archivo de salida
	outputFile := fmt.Sprintf("archivos_%s.txt", username)
	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo crear el archivo de lista: %v", err)
		return err
	}
	defer file.Close()

	// Escribir los archivos en el archivo, eliminando la extensión ".enc"
	file.WriteString("Lista de archivos de " + username + ":\n\n")
	for _, fileName := range resp.Filenames {
		cleanName := fileName
		if filepath.Ext(fileName) == ".enc" {
			cleanName = fileName[:len(fileName)-4] // Elimina la extensión ".enc"
		}
		_, err := file.WriteString(cleanName + "\n")
		if err != nil {
			log.Fatalf("[ERROR] No se pudo escribir en el archivo de lista: %v", err)
			return err
		}
	}

	log.Printf("[SUCCESS] Lista de archivos guardada en '%s'", outputFile)

	elapsed := time.Since(startTime)
	log.Printf("[INFO] Listado completado en %.2f s", elapsed.Seconds())
	return nil
}

func deleteFile(client pb.SyncServiceClient, filePath string, ctx context.Context) error {
	startTime := time.Now()

	// Extraer solo el nombre base del archivo
	filename := filepath.Base(filePath)

	log.Printf("[INFO] (%s) Eliminando %s del servidor...", time.Now().Format("15:04:05"), filename)

	_, err := client.DeleteFile(ctx, &pb.FileRequest{Filename: filename})
	if err != nil {
		log.Printf("[ERROR] No se pudo eliminar el archivo en el servidor: %v", err)
		return err
	}

	elapsed := time.Since(startTime)
	log.Printf("[SUCCESS] (%s) Archivo %s eliminado en %.2f s", time.Now().Format("15:04:05"), filename, elapsed.Seconds())
	return nil
}

func login(client pb.AuthServiceClient, username, password string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.Login(ctx, &pb.LoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		log.Fatalf("[ERROR] No se pudo autenticar: %v", err)
	}
	log.Println("[SUCCESS] Token obtenido:", resp.Token)
	return resp.Token, resp.RefreshToken, nil
}

func watchDirectory(syncClient pb.SyncServiceClient, dirPath string, ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("[ERROR] No se pudo iniciar el watcher: %v", err)
	}
	defer watcher.Close()

	// Agregar el directorio a la lista de observados
	err = watcher.Add(dirPath)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo observar el directorio: %v", err)
	}

	log.Println("[INFO] Monitoreando cambios en:", dirPath)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Detectar si es creación o modificación
			if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("[INFO] Detectado nuevo archivo/modificación: %s", event.Name)
				uploadFile(syncClient, event.Name, ctx)
			}
			// Detectar eliminación de archivos
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				log.Printf("[INFO] (%s) Detectada eliminación: %s", time.Now().Format("15:04:05"), event.Name)
				deleteFile(syncClient, event.Name, ctx)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[ERROR] Error en watcher: %v", err)
		}
	}
}

func listenForUpdates(client pb.SyncServiceClient, ctx context.Context) {
	stream, err := client.SyncUpdates(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("[ERROR] No se pudo conectar al stream de actualizaciones: %v", err)
	}

	log.Println("[INFO] Escuchando actualizaciones del servidor...")

	for {
		update, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("[ERROR] Error al recibir actualización: %v", err)
		}

		log.Printf("[INFO] (%s) Cambio en el servidor: %s - %s", time.Now().Format("15:04:05"), update.Action, update.Filename)

		if update.Action == "created" {
			downloadFile(client, update.Filename, ctx)
		}
	}
}

func retryUpload(client pb.SyncServiceClient, filePath string, ctx context.Context, retries int) error {
	for i := 1; i <= retries; i++ {
		uploadFile(client, filePath, ctx)

		log.WithFields(logrus.Fields{
			"file":   filePath,
			"attempt": i,
		}).Warn("Intento fallido de subida, reintentando...")

		time.Sleep(time.Duration(i*2) * time.Second) // Espera exponencial
	}
	return fmt.Errorf("fallaron todos los intentos de subida")
}

func getUsernameFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return "", fmt.Errorf("No se encontraron metadatos de autenticación")
	}

	tokens := md["authorization"]
	if len(tokens) == 0 {
		return "", fmt.Errorf("No se encontró token de autenticación")
	}

	token := tokens[0]
	_, claims, err := auth.ValidateToken(token) // Debes asegurarte de tener `ValidateToken` en `auth`
	if err != nil {
		return "", fmt.Errorf("Token inválido: %v", err)
	}

	username, ok := claims["username"].(string)
	if !ok {
		return "", fmt.Errorf("No se pudo extraer el nombre de usuario del token")
	}

	return username, nil
}


// 🛠️ Configurar la CLI
func main() {
	app := &cli.App{
		Name:  "SyncService Client",
		Usage: "Cliente gRPC para sincronización de archivos",
		Commands: []cli.Command{
			{
				Name:    "upload",
				Aliases: []string{"u"},
				Usage:   "Subir un archivo al servidor",
				Action: func(c *cli.Context) error {
					if c.NArg() < 1 {
						return fmt.Errorf("debes proporcionar la ruta del archivo")
					}
					filePath := c.Args().First()
					conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
					if err != nil {
						return err
					}
					defer conn.Close()

					syncClient := pb.NewSyncServiceClient(conn)
					authClient := pb.NewAuthServiceClient(conn)
					ctx, err := getAuthContext(authClient)
					if err != nil {
						return err
					}

					return uploadFile(syncClient, filePath, ctx)
				},
			},
			{
				Name:    "download",
				Aliases: []string{"d"},
				Usage:   "Descargar un archivo del servidor",
				Action: func(c *cli.Context) error {
					if c.NArg() < 1 {
						return fmt.Errorf("debes proporcionar el nombre del archivo")
					}
					filename := c.Args().First()
					conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
					if err != nil {
						return err
					}
					defer conn.Close()

					syncClient := pb.NewSyncServiceClient(conn)
					authClient := pb.NewAuthServiceClient(conn)
					ctx, err := getAuthContext(authClient)
					if err != nil {
						return err
					}

					return downloadFile(syncClient, filename, ctx)
				},
			},{
				Name:    "list",
				Aliases: []string{"l"},
				Usage:   "Listar los archivos del servidor",
				Action: func(c *cli.Context) error {
					conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
					if err != nil {
						return err
					}
					defer conn.Close()

					syncClient := pb.NewSyncServiceClient(conn)
					authClient := pb.NewAuthServiceClient(conn)
					ctx, err := getAuthContext(authClient)
					if err != nil {
						return err
					}
					
					return listFiles(syncClient, ctx)
				},
			},{
				Name:    "delete",
				Aliases: []string{"del"},
				Usage:   "Eliminar un archivo del servidor",
				Action: func(c *cli.Context) error {
					if c.NArg() < 1 {
						return fmt.Errorf("debes proporcionar el nombre del archivo")
					}
					filename := c.Args().First()
					conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
					if err != nil {
						return err
					}
					defer conn.Close()

					syncClient := pb.NewSyncServiceClient(conn)
					authClient := pb.NewAuthServiceClient(conn)
					ctx, err := getAuthContext(authClient)
					if err != nil {
						return err
					}

					return deleteFile(syncClient, filename, ctx)
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}