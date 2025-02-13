package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func uploadFile(client pb.SyncServiceClient, filePath string, ctx context.Context) {
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
	resp, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("[ERROR] Error al cerrar la subida: %v", err)
	}
	log.Println("[SUCCESS] Archivo subido con compresión:", resp.Message)
}

func downloadFile(client pb.SyncServiceClient, filename string, ctx context.Context) {
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

	log.Printf("[SUCCESS] Archivo %s descargado y descomprimido", filename)
}

func login(client pb.AuthServiceClient, username, password string) string {
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
	return resp.Token
}

func main() {
	// Definir flags CLI
	uploadCmd := flag.String("upload", "", "Sube un archivo al servidor")
	downloadCmd := flag.String("download", "", "Descarga un archivo desde el servidor")
	listCmd := flag.Bool("list", false, "Lista los archivos en el servidor")
	flag.Parse()

	// Conectar al servidor gRPC
	startTime := time.Now()
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("[ERROR] No se pudo conectar: %v", err)
	}
	defer conn.Close()
	authClient := pb.NewAuthServiceClient(conn)
	syncClient := pb.NewSyncServiceClient(conn)

	token := login(authClient, "admin", "password")

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", token))


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

	case *listCmd:
		log.Println("[INFO] Listando archivos en el servidor...")

		resp, err := syncClient.ListFiles(ctx, &pb.Empty{})
		if err != nil {
			log.Fatalf("[ERROR] Error en ListFiles: %v", err)
		}
		log.Println("[SUCCESS] Archivos en el servidor:", resp.Filenames)

	default:
		log.Println("[INFO] Uso:")
		log.Println("  - Para subir un archivo:   go run client/client.go --upload nombre_archivo.txt")
		log.Println("  - Para descargar un archivo: go run client/client.go --download nombre_archivo.txt")
		log.Println("  - Para listar archivos:    go run client/client.go --list")
	}

	totalTime := time.Since(startTime)
	log.Printf("[INFO] Tiempo total de ejecución: %v", totalTime)
}