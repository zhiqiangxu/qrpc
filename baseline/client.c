#include <sys/types.h>
#include <sys/socket.h>
#include <stdio.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <sys/shm.h>
#include<time.h>
#define MYPORT  8887
#define BUFFER_SIZE 1024
char *SERVER_IP = "127.0.0.1";
#define ONE_SEC (1000000000UL)
void delay_ns(unsigned long delay)
{
	struct  timespec start, end;
	clock_gettime(CLOCK_MONOTONIC,&start);
	start.tv_nsec = start.tv_sec * 1000000000 + start.tv_nsec;
repeat:
	clock_gettime(CLOCK_MONOTONIC,&end);
	end.tv_nsec = end.tv_sec * 1000000000 + end.tv_nsec;
	if(end.tv_nsec <  (start.tv_nsec + delay))
		goto repeat;
}
int main()
{
	char recvBuf[BUFFER_SIZE];
	char sendBuf[BUFFER_SIZE];
	struct sockaddr_in client_sockaddr;
	int client_sockfd = socket(AF_INET,SOCK_STREAM,0);
	struct  timespec start, end;
	int ret, coFd, flags;
	client_sockaddr.sin_family = AF_INET;
	client_sockaddr.sin_port = htons(MYPORT);
	client_sockaddr.sin_addr.s_addr = inet_addr(SERVER_IP);
	coFd = connect(client_sockfd,(struct sockaddr*)&client_sockaddr,
			sizeof(client_sockaddr));
	if(coFd == -1)
	{
		perror("connect");
		exit(1);
	}
	flags = fcntl(client_sockfd, F_GETFL,0);
	fcntl(client_sockfd, F_SETFL,flags|O_NONBLOCK);
	printf("connect successfully\n");
	sendBuf[0] = 'a';
	while(1) {
		clock_gettime(CLOCK_MONOTONIC,&start);
resend:
		ret = send(client_sockfd,sendBuf,100,0);
		if(ret < 0)
			goto resend;
repeat:
		ret = recv(client_sockfd,recvBuf,100,0);
		if(ret <= 0) {
			goto repeat;
		}
		clock_gettime(CLOCK_MONOTONIC,&end);
		start.tv_nsec = start.tv_sec * 1000000000 + start.tv_nsec;
		end.tv_nsec = end.tv_sec * 1000000000 + end.tv_nsec;
		//if(end.tv_nsec- start.tv_nsec > 100000)
		printf("\ncost time:%ld, %c, %d", end.tv_nsec - start.tv_nsec,
				recvBuf[0], ret);
		sendBuf[0] = sendBuf[0]  + 1;
		if(sendBuf[0] > 'z')
			sendBuf[0] = 'a';
		delay_ns(ONE_SEC/1);
	}
}
