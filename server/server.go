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
	"github.com/FelipeMarchantVargas/sync-service/server/auth"
	"github.com/fsnotify/fsnotify"
	"github.com/golang-jwt/jwt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const storageDir = "./storage"

type SyncServer struct {
	pb.UnimplementedSyncServiceServer
	updateClients []pb.SyncService_SyncUpdatesServer
}

type AuthServer struct {
	pb.UnimplementedAuthServiceServer
}

func (s *SyncServer) SyncUpdates(req *pb.Empty, stream pb.SyncService_SyncUpdatesServer) error {
	s.updateClients = append(s.updateClients, stream)

	// Mantiene el stream abierto
	<-stream.Context().Done()
	return nil
}

// Función para notificar cambios a los clientes conectados
func (s *SyncServer) notifyClients(filename string, action string) {
	for _, client := range s.updateClients {
		client.Send(&pb.FileUpdate{Filename: filename, Action: action})
	}
}

// Iniciar watcher en el directorio del servidor
func (s *SyncServer) watchServerDirectory() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("[ERROR] No se pudo iniciar el watcher en el servidor: %v", err)
	}
	defer watcher.Close()

	err = watcher.Add(storageDir)
	if err != nil {
		log.Fatalf("[ERROR] No se pudo observar el directorio: %v", err)
	}

	log.Println("[INFO] Monitoreando cambios en el servidor...")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			filename := filepath.Base(event.Name)

			if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("[INFO] Archivo creado/modificado en el servidor: %s", filename)
				s.notifyClients(filename, "created")
			}

			if event.Op&fsnotify.Remove == fsnotify.Remove {
				log.Printf("[INFO] Archivo eliminado en el servidor: %s", filename)
				s.notifyClients(filename, "deleted")
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[ERROR] Error en watcher del servidor: %v", err)
		}
	}
}

func (s *SyncServer) ListFiles(ctx context.Context, req *pb.Empty) (*pb.FileList, error) {
	username, err := getUsernameFromContext(ctx)
	if err != nil {
		return nil, err
	}
	userStorageDir := filepath.Join(storageDir, username)
	files, err := os.ReadDir(userStorageDir)
	if err != nil {
		log.Printf("[ERROR] No se pudo leer el directorio de %s: %v", username, err)
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
	username, err := getUsernameFromContext(stream.Context())
	if err != nil {
		return err
	}

	userStorageDir := filepath.Join(storageDir, username)
	if _, err := os.Stat(userStorageDir); os.IsNotExist(err) {
		os.Mkdir(userStorageDir, os.ModePerm)
	}

	startTime := time.Now()
	log.Printf("[INFO] (%s) %s está subiendo un archivo", time.Now().Format("15:04:05"), username)

	var filename string
	fileBuffer := []byte{}

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			safeFilename := filepath.Base(filename)

			// Descomprimir el archivo antes de guardarlo
			reader, err := gzip.NewReader(bytes.NewReader(fileBuffer))
			if err != nil {
				log.Printf("[ERROR] Error al descomprimir archivo %s: %v", safeFilename, err)
				return err
			}

			decompressedBuffer, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				log.Printf("[ERROR] Error al leer archivo descomprimido %s: %v", safeFilename, err)
				return err
			}

			// Guardar el archivo sin la extensión .gz
			filePath := filepath.Join(userStorageDir, safeFilename[:len(safeFilename)-3])
			err = os.WriteFile(filePath, decompressedBuffer, 0644)
			if err != nil {
				log.Printf("[ERROR] Error al guardar archivo %s: %v", safeFilename, err)
				return err
			}

			elapsed := time.Since(startTime)
			fileSize := len(decompressedBuffer)
			log.Printf("[SUCCESS] (%s) %s recibido (%d KB) y guardado en %.2f s", time.Now().Format("15:04:05"), safeFilename, fileSize/1024, elapsed.Seconds())
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
	username, err := getUsernameFromContext(stream.Context())
	if err != nil {
		return err
	}

	if err := authenticate(stream.Context()); err != nil {
		return err
	}
	userStorageDir := filepath.Join(storageDir, username)
	filePath := filepath.Join(userStorageDir, req.Filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return status.Errorf(codes.NotFound, "El archivo no existe o no tienes permiso")
	}

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

	log.Printf("[SUCCESS] Archivo %s enviado a %s", req.Filename, username)
	return nil
}

func (s *SyncServer) DeleteFile(ctx context.Context, req *pb.FileRequest) (*pb.UploadResponse, error) {
	username, err := getUsernameFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := authenticate(ctx); err != nil {
		return nil, err
	}

	userStorageDir := filepath.Join(storageDir, username)
	filePath := filepath.Join(userStorageDir, req.Filename)

	// Verificar si el archivo existe
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("[ERROR] Archivo %s no encontrado en el servidor.", req.Filename)
		return nil, status.Errorf(codes.NotFound, "El archivo %s no existe en el servidor", req.Filename)
	}

	// Eliminar el archivo
	err = os.Remove(filePath)
	if err != nil {
		log.Printf("[ERROR] No se pudo eliminar %s: %v", req.Filename, err)
		return nil, status.Errorf(codes.Internal, "Error al eliminar %s", req.Filename)
	}

	log.Printf("[SUCCESS] Archivo %s eliminado por %s", req.Filename, username)

	return &pb.UploadResponse{Message: "Archivo eliminado correctamente"}, nil
}

func (s *AuthServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	// ⚠️ En producción, valida con una base de datos
	if req.Username != "admin" || req.Password != "password" {
		return nil, status.Errorf(codes.Unauthenticated, "Credenciales incorrectas")
	}

	accessToken, err := auth.GenerateToken(req.Username)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error generando el token")
	}

	refreshToken, err := auth.GenerateRefreshToken(req.Username)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error generando el refresh token")
	}

	return &pb.LoginResponse{Token: accessToken, RefreshToken: refreshToken}, nil
}

func (s *AuthServer) RefreshToken(ctx context.Context, req *pb.RefreshRequest) (*pb.LoginResponse, error) {
	token, _, err := auth.ValidateToken(req.RefreshToken)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "Refresh token inválido o expirado")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "Error en claims")
	}

	username, ok := claims["username"].(string)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "Error obteniendo username")
	}

	// Generar nuevo Access Token
	accessToken, err := auth.GenerateToken(username)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error generando el token")
	}

	// Generar un nuevo Refresh Token
	newRefreshToken, err := auth.GenerateRefreshToken(username)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error generando el refresh token")
	}

	return &pb.LoginResponse{Token: accessToken, RefreshToken: newRefreshToken}, nil
}

func authenticate(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "Falta token de autenticación")
	}

	tokens := md["authorization"]
	if len(tokens) == 0 {
		return status.Errorf(codes.Unauthenticated, "Token no encontrado")
	}

	token := tokens[0]
	_, _, err := auth.ValidateToken(token)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "Token inválido")
	}

	return nil
}

// Obtener el nombre de usuario desde el contexto gRPC
func getUsernameFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Errorf(codes.Unauthenticated, "Falta token de autenticación")
	}

	tokens := md["authorization"]
	if len(tokens) == 0 {
		return "", status.Errorf(codes.Unauthenticated, "Token no encontrado")
	}

	token := tokens[0]
	_, claims, err := auth.ValidateToken(token)
	if err != nil {
		return "", status.Errorf(codes.Unauthenticated, "Token inválido")
	}

	username, ok := claims["username"].(string)
	if !ok {
		return "", status.Errorf(codes.Unauthenticated, "El token no contiene un username válido")
	}

	return username, nil
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
	syncServer := &SyncServer{}
	pb.RegisterSyncServiceServer(grpcServer, syncServer)
	pb.RegisterAuthServiceServer(grpcServer, &AuthServer{})

	// Iniciar watcher en el servidor
	go syncServer.watchServerDirectory()

	log.Println("Servidor gRPC corriendo en el puerto 50051")
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Error al iniciar el servidor gRPC: %v", err)
	}
}
