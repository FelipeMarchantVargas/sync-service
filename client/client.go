package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"google.golang.org/grpc"
)

func uploadFile(client pb.SyncServiceClient, filePath string) {
	// Abrir el archivo para leerlo
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error al abrir el archivo: %v", err)
	}
	defer file.Close()

	// Crear un stream para enviar el archivo al servidor
	stream, err := client.UploadFile(context.Background())
	if err != nil {
		log.Fatalf("Error al iniciar la subida: %v", err)
	}

	buffer := make([]byte, 1024) // Tamaño de fragmento (1KB)

	for {
		// Leer un fragmento del archivo
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Error al leer el archivo: %v", err)
		}

		// Enviar el fragmento al servidor
		err = stream.Send(&pb.FileChunk{
			Filename: filepath.Base(filePath),
			Data:     buffer[:n],
		})
		if err != nil {
			log.Fatalf("Error al enviar fragmento: %v", err)
		}
	}

	// Cerrar la transmisión y recibir respuesta del servidor
	resp, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("Error al cerrar la subida: %v", err)
	}

	log.Println("Respuesta del servidor:", resp.Message)
}

func downloadFile(client pb.SyncServiceClient, filename string) {
	// Solicitar el archivo al servidor
	stream, err := client.DownloadFile(context.Background(), &pb.FileRequest{Filename: filename})
	if err != nil {
		log.Fatalf("Error al solicitar archivo: %v", err)
	}

	// Crear el archivo en el sistema local
	filePath := filepath.Join("./", filename)
	file, err := os.Create(filePath)
	if err != nil {
		log.Fatalf("Error al crear archivo local: %v", err)
	}
	defer file.Close()

	for {
		// Recibir un fragmento del archivo
		chunk, err := stream.Recv()
		if err == io.EOF {
			break // Archivo completo
		}
		if err != nil {
			log.Fatalf("Error al recibir fragmento: %v", err)
		}

		// Escribir el fragmento en el archivo local
		_, err = file.Write(chunk.Data)
		if err != nil {
			log.Fatalf("Error al escribir en el archivo: %v", err)
		}
	}

	log.Printf("Archivo %s descargado exitosamente", filename)
}


func main() {
	// Conectar al servidor gRPC
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("No se pudo conectar: %v", err)
	}
	defer conn.Close()

	client := pb.NewSyncServiceClient(conn)

	// Listar archivos disponibles en el servidor
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.ListFiles(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("Error en ListFiles: %v", err)
	}
	log.Println("Archivos en el servidor:", resp.Filenames)

	// Subir un archivo
	uploadFile(client, "testfile.txt")

	// Volver a listar archivos
	resp, err = client.ListFiles(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("Error en ListFiles después de subir: %v", err)
	}
	log.Println("Archivos después de subir:", resp.Filenames)

	// Descargar el archivo subido
	downloadFile(client, "testfile.txt")
}
