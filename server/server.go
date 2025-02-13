package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	pb "github.com/FelipeMarchantVargas/sync-service/proto"
	"google.golang.org/grpc"
)

const storageDir = "./storage"

type SyncServer struct {
	pb.UnimplementedSyncServiceServer
}

func (s *SyncServer) ListFiles(ctx context.Context, req *pb.Empty) (*pb.FileList, error) {
	files, err := os.ReadDir(storageDir)
	if err != nil {
		log.Printf("Error al leer el directorio de almacenamiento: %v", err)
		return nil, err
	}

	var filenames []string
	for _, file := range files {
		if !file.IsDir() {
			filenames = append(filenames, file.Name())
		}
	}

	return &pb.FileList{Filenames: filenames}, nil
}

func (s *SyncServer) UploadFile(stream pb.SyncService_UploadFileServer) error {
	startTime := time.Now()
	log.Println("[INFO] Cliente inició la subida de un archivo comprimido.")

	var filename string
	fileBuffer := []byte{}

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			// Descomprimir el archivo antes de guardarlo
			reader, err := gzip.NewReader(bytes.NewReader(fileBuffer))
			if err != nil {
				log.Printf("[ERROR] Error al descomprimir archivo %s: %v", filename, err)
				return err
			}

			decompressedBuffer, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				log.Printf("[ERROR] Error al leer archivo descomprimido %s: %v", filename, err)
				return err
			}

			// Guardar el archivo sin la extensión .gz
			filePath := filepath.Join(storageDir, filename[:len(filename)-3])
			err = os.WriteFile(filePath, decompressedBuffer, 0644)
			if err != nil {
				log.Printf("[ERROR] Error al guardar archivo %s: %v", filename, err)
				return err
			}

			elapsed := time.Since(startTime)
			log.Printf("[SUCCESS] Archivo %s recibido, descomprimido y guardado en %v", filePath, elapsed)
			return stream.SendAndClose(&pb.UploadResponse{
				Message: "Archivo subido y descomprimido con éxito",
			})
		}

		if err != nil {
			log.Printf("[ERROR] Error al recibir fragmento: %v", err)
			return err
		}

		// Guardar el nombre del archivo (solo una vez)
		if filename == "" {
			filename = chunk.Filename
		}

		// Agregar el fragmento al buffer
		fileBuffer = append(fileBuffer, chunk.Data...)
	}
}

func (s *SyncServer) DownloadFile(req *pb.FileRequest, stream pb.SyncService_DownloadFileServer) error {
	startTime := time.Now()
	filePath := filepath.Join(storageDir, req.Filename)

	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("[ERROR] No se pudo abrir el archivo %s: %v", req.Filename, err)
		return err
	}
	defer file.Close()

	// Comprimir el archivo en memoria
	var compressedBuffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuffer)

	_, err = io.Copy(gzipWriter, file)
	gzipWriter.Close()
	if err != nil {
		log.Printf("[ERROR] No se pudo comprimir el archivo %s: %v", req.Filename, err)
		return err
	}

	buffer := compressedBuffer.Bytes()
	chunkSize := 1024
	for i := 0; i < len(buffer); i += chunkSize {
		end := i + chunkSize
		if end > len(buffer) {
			end = len(buffer)
		}

		err := stream.Send(&pb.FileChunk{
			Filename: req.Filename + ".gz",
			Data:     buffer[i:end],
		})
		if err != nil {
			log.Printf("[ERROR] Error al enviar fragmento del archivo %s: %v", req.Filename, err)
			return err
		}
	}

	elapsed := time.Since(startTime)
	log.Printf("[SUCCESS] Archivo %s comprimido y enviado en %v", req.Filename, elapsed)
	return nil
}



func main() {

	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		os.Mkdir(storageDir, os.ModePerm)
	}
	
	// Escuchar en el puerto 50051
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Error al iniciar el servidor: %v", err)
	}

	// Crear el servidor gRPC
	grpcServer := grpc.NewServer()
	pb.RegisterSyncServiceServer(grpcServer, &SyncServer{})

	log.Println("Servidor gRPC corriendo en el puerto 50051")
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Error al iniciar el servidor gRPC: %v", err)
	}
}
