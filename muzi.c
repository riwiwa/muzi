#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <zip.h>
#include <cjson/cJSON.h>
#include <sys/stat.h>
#include <sys/types.h>

// TODO:
// - unzip a given .zip archive of spotify data to a directory
// - for each json file in the directory {
//  - for each json entry {
//    - add entry to postgresql db
//  }
// }
//
// - web ui
// - sql tables: "full history", "artists", "songs", "albums" (see ipad)

int extract(const char *path);
int get_artist_plays(void);

int get_artist_plays(void) {
  FILE *fp = fopen("/home/r/dl/spotify-data/rm35@gm - Spotify Extended Streaming History/Streaming_History_Audio_2020-2021_4.json", "r");
  if(fp == NULL) {
    printf("Error while opening file\n");
    return 1;
  }

  fseek(fp, 0, SEEK_END);
  long fileSize = ftell(fp);
  fseek(fp, 0, SEEK_SET);

  char *buffer = (char *)malloc(fileSize + 1);
  fread(buffer, 1, fileSize, fp);
  buffer[fileSize] = '\0';
  fclose(fp);

  cJSON *json = cJSON_Parse(buffer);
  if(json == NULL) {
    const char *error_ptr = cJSON_GetErrorPtr();
    if (error_ptr != NULL) {
      printf("Error: %s\n", error_ptr);
    }
    cJSON_Delete(json);
    free(buffer);
    return 1;
  }

  cJSON *track = NULL;
  int i = 0;
  char artist[] = "";
  cJSON_ArrayForEach(track, json) {
    cJSON *trackName = cJSON_GetObjectItemCaseSensitive(track, "master_metadata_album_artist_name");
    if(cJSON_IsString(trackName)) {
      if(strcasecmp(artist, (trackName->valuestring)) == 0) {
        i++;
      } 
    }
  }
  printf("\"%s\" count: %d\n", artist, i);


  cJSON_Delete(json);
  free(buffer);
  return 0;
}

int extract(const char *path) {
  zip_t *za;
  int err;
  if((za = zip_open(path, 0, &err)) == NULL) {
    zip_error_t error;
    zip_error_init_with_code(&error, err);
    fprintf(stderr, "Error opening zip archive: %s\n", zip_error_strerror(&error));
    zip_error_fini(&error);
    return 1;
  }

  int archived_files = zip_get_num_entries(za, ZIP_FL_UNCHANGED);

  for (int i = 0; i < archived_files; i++) {
    struct zip_stat st;
    if (zip_stat_index(za, i, 0, &st) < 0) {
      fprintf(stderr, "Error getting file info for index %d\n", i);
      continue;
    }

    // Handle directories
    if (st.name[strlen(st.name) - 1] == '/') {
      mkdir(st.name, 0755); // Create directory if it doesn't exist
      continue;
    }

    // Open file in archive
    zip_file_t *zf = zip_fopen_index(za, i, 0);
    if (!zf) {
      fprintf(stderr, "Error opening file in zip: %s\n", st.name);
      continue;
    }

    // Create output file
    FILE *outfile = fopen(st.name, "w+");
    if (!outfile) {
      fprintf(stderr, "Error creating output file: %s\n", st.name);
      zip_fclose(zf);
      continue;
    }

    // Read and write data
    char buffer[4096];
    zip_int64_t bytes_read;
    while ((bytes_read = zip_fread(zf, buffer, sizeof(buffer))) > 0) {
      fwrite(buffer, 1, bytes_read, outfile);
    }

    // Close files
    fclose(outfile);
    zip_fclose(zf);
  }
  return 0;
}

int main(void) {
  extract("archive.zip");
}
