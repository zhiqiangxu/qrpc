#include<sys/types.h>
#include<sys/socket.h>
#include<stdio.h>
#include<netinet/in.h>
#include<arpa/inet.h>
#include<unistd.h>
#include<string.h>
#include<stdlib.h>
#include<fcntl.h>
#include<sys/shm.h>
#define MYPORT  8887
#define QUEUE   20
#define BUFFER_SIZE 1024
int main()
{
	int server_sockfd = socket(AF_INET,SOCK_STREAM,0);
	struct sockaddr_in server_sockaddr;
	server_sockaddr.sin_family = AF_INET;
	server_sockaddr.sin_port = htons(MYPORT);
	server_sockaddr.sin_addr.s_addr = htonl(INADDR_ANY);
	int newFd, len, flags;
	struct sockaddr_in client_sockaddr;
	socklen_t length = sizeof(client_sockaddr);
	char buffer[BUFFER_SIZE];
	if(bind(server_sockfd,(struct sockaddr*)&server_sockaddr,
				sizeof(server_sockaddr)) == -1){
  		perror("bind");
		exit(1);
	}
	if(listen(server_sockfd,QUEUE) == -1) {
  		perror("listen");
		exit(1);
	}
	printf("waiting for connection\n");
	newFd = accept(server_sockfd,(struct sockaddr*)&client_sockaddr,
			&length);
	if(newFd == -1){
		perror("connect");
		exit(1);
	}
	flags = fcntl(newFd, F_GETFL,0);
	fcntl(newFd, F_SETFL,flags|O_NONBLOCK);
	printf("connect successfully\n");
	while(1) {
		len = recv(newFd,buffer,sizeof(buffer),0);
		if(len <= 0)
			continue;
		send(newFd,buffer,len,0);
	}
}
