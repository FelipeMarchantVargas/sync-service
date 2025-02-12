package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"

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
	log.Println("Cliente inició la subida de un archivo.")

	var filename string
	fileBuffer := []byte{}

	for {
		// Recibir fragmento del cliente
		chunk, err := stream.Recv()
		if err == io.EOF {
			// Guardar el archivo cuando termina la transmisión
			filePath := filepath.Join(storageDir, filename)
			err := os.WriteFile(filePath, fileBuffer, 0644)
			if err != nil {
				log.Printf("Error al guardar el archivo %s: %v", filename, err)
				return err
			}
			log.Printf("Archivo %s recibido y guardado correctamente", filename)
			return stream.SendAndClose(&pb.UploadResponse{
				Message: "Archivo subido con éxito",
			})
		}
		if err != nil {
			log.Printf("Error al recibir fragmento de archivo: %v", err)
			return err
		}

		// Guardar el nombre del archivo (solo la primera vez)
		if filename == "" {
			filename = chunk.Filename
		}

		// Agregar los datos al buffer
		fileBuffer = append(fileBuffer, chunk.Data...)
	}
}

func (s *SyncServer) DownloadFile(req *pb.FileRequest, stream pb.SyncService_DownloadFileServer) error {
	filePath := filepath.Join(storageDir, req.Filename)

	// Abrir el archivo para lectura
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error al abrir el archivo %s: %v", req.Filename, err)
		return err
	}
	defer file.Close()

	buffer := make([]byte, 1024) // Tamaño del fragmento (1KB)

	for {
		// Leer un fragmento del archivo
		n, err := file.Read(buffer)
		if err == io.EOF {
			break // Fin del archivo
		}
		if err != nil {
			log.Printf("Error al leer el archivo %s: %v", req.Filename, err)
			return err
		}

		// Enviar el fragmento al cliente
		err = stream.Send(&pb.FileChunk{
			Filename: req.Filename,
			Data:     buffer[:n],
		})
		if err != nil {
			log.Printf("Error al enviar fragmento del archivo %s: %v", req.Filename, err)
			return err
		}
	}

	log.Printf("Archivo %s enviado correctamente", req.Filename)
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
