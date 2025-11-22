CC=gcc
CFLAGS=-Wall -Wextra -Wpedantic -std=c11 -fdiagnostics-color=always -D_DEFAULT_SOURCE -g -I./include
LIBS=-Wl,--no-as-needed -lcjson -lzip -lpq
TARGET=muzi
SOURCES = muzi.c
OBJECTS = $(SOURCES:.c=.o)

OUTDIR=./build
OBJDIR=$(OUTDIR)/obj

$(shell mkdir -p $(OBJDIR))

%.o: %.c
	$(CC) -c -o $(OBJDIR)/$@ $< $(CFLAGS)

$(TARGET): $(OBJECTS)
	$(CC) -o $(OUTDIR)/$@ $(OBJDIR)/$(OBJECTS) $(CFLAGS) $(LIBS)

.PHONY: all
all: $(TARGET)

.DEFAULT_GOAL := all

clean:
	rm -rf $(OUTDIR)
