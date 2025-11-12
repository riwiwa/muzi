#include <cjson/cJSON.h>
#include <dirent.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <zip.h>

#define MAX_FILENAME_SIZE 255

// TODO:
// - for each json file in the directory {
//  - for each json entry {
//    - add entry to postgresql db
//  }
// }
//
// - web ui
// - sql tables: "full history", "artists", "songs", "albums" (see ipad)
//

int extract(const char *path, const char *target);
int get_artist_plays(void);
int import_spotify(void);

int get_artist_plays(void) {
  FILE *fp = fopen("test.json", "r");
  if (fp == NULL) {
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
  if (json == NULL) {
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
    cJSON *trackName = cJSON_GetObjectItemCaseSensitive(
        track, "master_metadata_album_artist_name");
    if (cJSON_IsString(trackName)) {
      if (strcasecmp(artist, (trackName->valuestring)) == 0) {
        i++;
      }
    }
  }
  printf("\"%s\" count: %d\n", artist, i);

  cJSON_Delete(json);
  free(buffer);
  return 0;
}

int extract(const char *path, const char *target) {
  mkdir(target, 0777);
  zip_t *za;
  int err;
  if ((za = zip_open(path, 0, &err)) == NULL) {
    zip_error_t error;
    zip_error_init_with_code(&error, err);
    fprintf(stderr, "Error opening zip archive: %s\n",
            zip_error_strerror(&error));
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

    char file_target[MAX_FILENAME_SIZE];
    snprintf(file_target, MAX_FILENAME_SIZE, "%s/%s", target, st.name);

    char *search = strchr(st.name, '/');
    if (search != NULL) {
      int index = search - st.name;
      int end = 0;
      char dir[strlen(st.name)];
      for (int j = 0; j < index; j++) {
        dir[j] = st.name[j];
        end = j;
      }
      dir[end + 1] = '\0';
      char dir_target[MAX_FILENAME_SIZE];
      snprintf(dir_target, MAX_FILENAME_SIZE, "%s/%s", target, dir);
      mkdir(dir_target, 0777);
    }

    // Handle directories
    if (file_target[strlen(file_target) - 1] == '/') {
      mkdir(file_target, 0777); // Create directory if it doesn't exist
      continue;
    }

    // Open file in archive
    zip_file_t *zf = zip_fopen_index(za, i, 0);
    if (!zf) {
      fprintf(stderr, "Error opening file in zip: %s\n", file_target);
      continue;
    }

    // Create output file
    FILE *outfile = fopen(file_target, "w+");
    if (!outfile) {
      fprintf(stderr, "Error creating output file: %s\n", file_target);
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

int import_spotify(void) {
  const char *path = "./spotify-data/zip";
  const char *target_base = "./spotify-data/extracted";
  DIR *dir = opendir(path);
  if (dir == NULL) {
    fprintf(stderr, "Error opening directory: %s (%d)\n", path, errno);
    return errno;
  }
  struct dirent *entry = NULL;
  while ((entry = readdir(dir)) != NULL) {
    char full_name[MAX_FILENAME_SIZE];
    snprintf(full_name, MAX_FILENAME_SIZE, "%s/%s", path, entry->d_name);

    if (entry->d_type != DT_DIR) {
      char *ext = strrchr(entry->d_name, '.');
      if (strcmp(ext, ".zip") == 0) {
        char *zip_dir_name = entry->d_name;
        int len = strlen(zip_dir_name);
        zip_dir_name[len - 4] = '\0';
        char target[MAX_FILENAME_SIZE];
        snprintf(target, MAX_FILENAME_SIZE, "%s/%s", target_base, zip_dir_name);
        extract(full_name, target);
      }
    }
  }

  closedir(dir);
  return 0;
}

int main(void) { import_spotify(); }
