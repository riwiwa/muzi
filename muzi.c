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
//  - for each json entry {
//    - add entry to postgresql db
//  }
//
// - web ui
// - sql tables: "full history", "artists", "songs", "albums" (see ipad)
//

int extract(const char *path, const char *target);
int get_artist_plays(const char *json_file, const char *artist);
int import_spotify(void);
int add_dir_to_db(const char *path);
int add_to_db(const char *json_file);

int add_to_db(const char *json_file) {
  // for json_entry in json_file {
  //  add to database
  // }
  printf("Adding to database: %s\n", json_file);
  return 0;
}

int add_dir_to_db(const char *path) {
  DIR *dir = opendir(path);
  struct dirent *data_dir = NULL;

  while ((data_dir = readdir(dir)) != NULL) {
    if (data_dir->d_type == DT_DIR) {
      if ((strcmp(data_dir->d_name, ".") != 0) &&
          (strcmp(data_dir->d_name, "..") != 0)) {
        char data_dir_path[MAX_FILENAME_SIZE];
        if (snprintf(data_dir_path, MAX_FILENAME_SIZE, "%s/%s/%s", path,
                     data_dir->d_name,
                     "Spotify Extended Streaming History") < 0) {
          return 1;
        }

        DIR *json_dir = opendir(data_dir_path);
        struct dirent *json_file = NULL;
        while ((json_file = readdir(json_dir)) != NULL) {
          if (json_file->d_type != DT_DIR) {
            char *ext = strrchr(json_file->d_name, '.');
            if (strcmp(ext, ".json") == 0) {
              char json_file_path[MAX_FILENAME_SIZE];
              if (snprintf(json_file_path, MAX_FILENAME_SIZE, "%s/%s",
                           data_dir_path, json_file->d_name) < 0) {
                return 1;
              }

              add_to_db(json_file_path);
            }
          }
        }
      }
    }
  }
  return 0;
}

int get_artist_plays(const char *json_file, const char *artist) {
  FILE *fp = fopen(json_file, "r");
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
    if (snprintf(file_target, MAX_FILENAME_SIZE, "%s/%s", target, st.name) <
        0) {
      return 1;
    }

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
      if (snprintf(dir_target, MAX_FILENAME_SIZE, "%s/%s", target, dir) < 0) {
        return 1;
      }
      mkdir(dir_target, 0777);
    }

    if (file_target[strlen(file_target) - 1] == '/') {
      mkdir(file_target, 0777);
      continue;
    }

    zip_file_t *zf = zip_fopen_index(za, i, 0);
    if (!zf) {
      fprintf(stderr, "Error opening file in zip: %s\n", file_target);
      continue;
    }

    FILE *outfile = fopen(file_target, "w+");
    if (!outfile) {
      fprintf(stderr, "Error creating output file: %s\n", file_target);
      zip_fclose(zf);
      continue;
    }

    char buffer[4096];
    zip_int64_t bytes_read;
    while ((bytes_read = zip_fread(zf, buffer, sizeof(buffer))) > 0) {
      fwrite(buffer, 1, bytes_read, outfile);
    }

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
    if (snprintf(full_name, MAX_FILENAME_SIZE, "%s/%s", path, entry->d_name) <
        0) {
      return 1;
    }

    if (entry->d_type != DT_DIR) {
      char *ext = strrchr(entry->d_name, '.');
      if (strcmp(ext, ".zip") == 0) {
        char *zip_dir_name = entry->d_name;
        int len = strlen(zip_dir_name);
        zip_dir_name[len - 4] = '\0';
        char target[MAX_FILENAME_SIZE];
        if (snprintf(target, MAX_FILENAME_SIZE, "%s/%s", target_base,
                     zip_dir_name) < 0) {
          return 1;
        }
        extract(full_name, target);
      }
    }
  }

  closedir(dir);
  add_dir_to_db(target_base);
  return 0;
}

int main(void) {
  // import_spotify();
  // add_dir_to_db("./spotify-data/extracted");
}
