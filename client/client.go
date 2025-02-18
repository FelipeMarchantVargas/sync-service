package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Estructura para almacenar credenciales
type Credentials struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

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

func uploadFile(client pb.SyncServiceClient, filePath string, ctx context.Context) {
	startTime := time.Now()
	
	// Extraer solo el nombre base del archivo
	filename := filepath.Base(filePath)

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
			Filename: filePath + ".gz",
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
}

func downloadFile(client pb.SyncServiceClient, filename string, ctx context.Context) {
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

	// Guardar el archivo descomprimido
	filePath := filepath.Join("./", filename)
	err = os.WriteFile(filePath, decompressedData, 0644)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo guardar el archivo: %v", err)
	}

	elapsed := time.Since(startTime)
	log.Printf("[SUCCESS] (%s) %s descargado en %.2f s", time.Now().Format("15:04:05"), filename, elapsed.Seconds())
}

func deleteFile(client pb.SyncServiceClient, filePath string, ctx context.Context) {
	startTime := time.Now()

	// Extraer solo el nombre base del archivo
	filename := filepath.Base(filePath)

	log.Printf("[INFO] (%s) Eliminando %s del servidor...", time.Now().Format("15:04:05"), filename)

	_, err := client.DeleteFile(ctx, &pb.FileRequest{Filename: filename})
	if err != nil {
		log.Printf("[ERROR] No se pudo eliminar el archivo en el servidor: %v", err)
		return
	}

	elapsed := time.Since(startTime)
	log.Printf("[SUCCESS] (%s) Archivo %s eliminado en %.2f s", time.Now().Format("15:04:05"), filename, elapsed.Seconds())
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

func main() {
	// Definir flags CLI
	uploadCmd := flag.String("upload", "", "Sube un archivo al servidor")
	downloadCmd := flag.String("download", "", "Descarga un archivo desde el servidor")
	listCmd := flag.Bool("list", false, "Lista los archivos en el servidor")
	watchCmd := flag.String("watch", "", "Monitorea un directorio y sincroniza archivos automáticamente")
	deleteCmd := flag.String("delete", "", "Elimina un archivo del servidor")
	flag.Parse()

	// Conectar al servidor gRPC
	startTime := time.Now()
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("[ERROR] No se pudo conectar: %v", err)
	}
	defer conn.Close()

	// crear clientes gRPC
	authClient := pb.NewAuthServiceClient(conn)
	syncClient := pb.NewSyncServiceClient(conn)

	// Obtener contexto autenticado con tokens
	ctx, err := getAuthContext(authClient)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo autenticar: %v", err)
	}

	// Ejecutar acción según el comando ingresado
	switch {
	case *uploadCmd != "":
		if _, err := os.Stat(*uploadCmd); os.IsNotExist(err) {
			log.Fatalf("[ERROR] El archivo %s no existe", *uploadCmd)
		}
		log.Printf("[INFO] Subiendo archivo: %s", *uploadCmd)
		start := time.Now()
		uploadFile(syncClient, *uploadCmd, ctx)
		elapsed := time.Since(start)
		log.Printf("[SUCCESS] Archivo subido en %v", elapsed)

	case *downloadCmd != "":
		log.Printf("[INFO] Descargando archivo: %s", *downloadCmd)
		start := time.Now()
		downloadFile(syncClient, *downloadCmd, ctx)
		elapsed := time.Since(start)
		log.Printf("[SUCCESS] Archivo descargado en %v", elapsed)

	case *deleteCmd != "":
		log.Printf("[INFO] Eliminando archivo: %s", *deleteCmd)
		start := time.Now()
		deleteFile(syncClient, *deleteCmd, ctx)
		elapsed := time.Since(start)
		log.Printf("[SUCCESS] Archivo eliminado en %v", elapsed)

	case *listCmd:
		log.Println("[INFO] Listando archivos en el servidor...")

		resp, err := syncClient.ListFiles(ctx, &pb.Empty{})
		if err != nil {
			log.Fatalf("[ERROR] Error en ListFiles: %v", err)
		}
		log.Println("[SUCCESS] Archivos en el servidor:", resp.Filenames)

	case *watchCmd != "":
		log.Println("[INFO] Iniciando sincronización automática en:", *watchCmd)

		go watchDirectory(syncClient, *watchCmd, ctx) // Monitorear cambios locales
		go listenForUpdates(syncClient, ctx)         // Escuchar cambios remotos

		select {} // Mantener el programa corriendo

	default:
		log.Println("[INFO] Uso:")
		log.Println("  - Para subir un archivo:   go run client/client.go --upload nombre_archivo.txt")
		log.Println("  - Para descargar un archivo: go run client/client.go --download nombre_archivo.txt")
		log.Println("  - Para listar archivos:    go run client/client.go --list")
		log.Println("  - Para monitorear un dir:  go run client/client.go --watch carpeta/")
	}

	totalTime := time.Since(startTime)
	log.Printf("[INFO] Tiempo total de ejecución: %v", totalTime)
}