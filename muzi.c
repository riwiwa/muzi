#include <cjson/cJSON.h>
#include <dirent.h>
#include <errno.h>
#include <libpq-fe.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <zip.h>

#define MAX_FILENAME_SIZE 255

enum Platform {
  SPOTIFY = 0,
  LASTFM = 1,
};

int extract(const char *path, const char *target);
int get_artist_plays(const char *json_file, const char *artist);
int import_spotify(void);
int add_dir_to_db(const char *path, int platform);
int json_to_db(const char *json_file, int platform);
int create_db(void);
bool db_exists(void);
bool table_exists(const char *name, PGconn *conn);
int create_table(const char *name, PGconn *conn);

bool table_exists(const char *name, PGconn *conn) {
  PGresult *result =
      PQexecParams(conn,
                   "SELECT EXISTS (SELECT 1 FROM pg_tables "
                   "WHERE schemaname = 'public' AND tablename = $1);",
                   1, NULL, &name, NULL, NULL, 0);
  if (PQresultStatus(result) != PGRES_TUPLES_OK) {
    printf("SELECT EXISTS failed: %s\n", PQerrorMessage(conn));
    PQclear(result);
    return false;
  }
  bool exists = strcmp(PQgetvalue(result, 0, 0), "t") == 0;
  PQclear(result);
  return exists;
}

bool db_exists(void) {
  PGconn *tmp_conn =
      PQconnectdb("host=localhost port=5432 dbname=postgres user=postgres "
                  "password=postgres");
  if (PQstatus(tmp_conn) != CONNECTION_OK) {
    printf("Temporary database connection failed: %s\n",
           PQerrorMessage(tmp_conn));
    PQfinish(tmp_conn);
    return false;
  }
  PGresult *result =
      PQexec(tmp_conn, "SELECT 1 FROM pg_database WHERE datname = 'muzi'");
  if (PQresultStatus(result) != PGRES_TUPLES_OK) {
    printf("SELECT command failed: %s\n", PQerrorMessage(tmp_conn));
    PQclear(result);
    PQfinish(tmp_conn);
    return false;
  }
  if (PQntuples(result) > 0) {
    return true;
  } else {
    return false;
  }
}

int create_db(void) {
  PGconn *tmp_conn =
      PQconnectdb("host=localhost port=5432 dbname=postgres user=postgres "
                  "password=postgres");
  if (PQstatus(tmp_conn) != CONNECTION_OK) {
    printf("Temporary database connection failed: %s\n",
           PQerrorMessage(tmp_conn));
    PQfinish(tmp_conn);
    return EXIT_FAILURE;
  }
  if (PQresultStatus(PQexec(tmp_conn, "CREATE DATABASE muzi")) !=
      PGRES_COMMAND_OK) {
    printf("CREATE DATABASE muzi failed: %s\n", PQerrorMessage(tmp_conn));
    PQfinish(tmp_conn);
    return EXIT_FAILURE;
  }
  PQfinish(tmp_conn);
  printf("muzi database created successfully.\n");
  return EXIT_SUCCESS;
}

int json_to_db(const char *json_file, int platform) {
  if (db_exists() == false) {
    create_db();
  }
  PGconn *conn =
      PQconnectdb("host=localhost port=5432 dbname=muzi user=postgres "
                  "password=postgres");
  if (PQstatus(conn) != CONNECTION_OK) {
    printf("Temporary database connection failed: %s\n", PQerrorMessage(conn));
    PQfinish(conn);
    return EXIT_FAILURE;
  }

  bool e = table_exists("history", conn);
  if (!e) {
    PGresult *result =
        PQexec(conn, "CREATE TABLE history ( ms_played INTEGER, timestamp "
                     "TIMESTAMPTZ, song_name "
                     "TEXT, artist TEXT, "
                     "album_name TEXT, PRIMARY KEY (timestamp, ms_played, "
                     "artist, song_name));");
    if (PQresultStatus(result) != PGRES_COMMAND_OK) {
      printf("History table creation failed: %s\n", PQerrorMessage(conn));
      PQclear(result);
      PQfinish(conn);
      return EXIT_FAILURE;
    }
    printf("Created history table.\n");
    PQclear(result);
  }

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
    return EXIT_FAILURE;
  }

  if (platform == SPOTIFY) {
    cJSON *play = NULL;
    cJSON_ArrayForEach(play, json) {
      // supports up to 2.7k hour long songs
      char ms_played[10];
      char *timestamp;
      char *song_name;
      char *album_artist;
      char *album;

      cJSON *ms_played_obj =
          cJSON_GetObjectItemCaseSensitive(play, "ms_played");
      if (cJSON_IsNumber(ms_played_obj)) {
        sprintf(ms_played, "%d", ms_played_obj->valueint);
      }
      // do not add to database if played for only 20 seconds
      if (ms_played_obj->valueint < 20000) {
        continue;
      }
      cJSON *timestamp_obj = cJSON_GetObjectItemCaseSensitive(play, "ts");
      if (cJSON_IsString(timestamp_obj)) {
        timestamp = timestamp_obj->valuestring;
      }
      cJSON *song_name_obj =
          cJSON_GetObjectItemCaseSensitive(play, "master_metadata_track_name");
      if (cJSON_IsString(song_name_obj)) {
        song_name = song_name_obj->valuestring;
      }
      cJSON *artist_obj = cJSON_GetObjectItemCaseSensitive(
          play, "master_metadata_album_artist_name");
      if (cJSON_IsString(artist_obj)) {
        album_artist = artist_obj->valuestring;
      }
      cJSON *album_obj = cJSON_GetObjectItemCaseSensitive(
          play, "master_metadata_album_album_name");
      if (cJSON_IsString(album_obj)) {
        album = album_obj->valuestring;
      }
      const char *data[5] = {timestamp, song_name, album_artist, album,
                             ms_played};
      PGresult *result =
          PQexecParams(conn,
                       "INSERT INTO history (timestamp, song_name, artist, "
                       "album_name, ms_played) VALUES ($1, $2, $3, $4, $5);",
                       5, NULL, data, NULL, NULL, 0);
      if (PQresultStatus(result) != PGRES_COMMAND_OK) {
        printf("Attempt to insert data for track failed: %s\n",
               PQerrorMessage(conn));
      }
      PQclear(result);
    }
  }

  PQfinish(conn);
  cJSON_Delete(json);
  free(buffer);
  printf("Added file: '%s' to muzi database.\n", json_file);
  return EXIT_SUCCESS;
}

int add_dir_to_db(const char *path, int platform) {
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
          return EXIT_FAILURE;
        }

        DIR *json_dir = opendir(data_dir_path);
        struct dirent *json_file = NULL;
        while ((json_file = readdir(json_dir)) != NULL) {
          if (json_file->d_type != DT_DIR) {
            char *ext = strrchr(json_file->d_name, '.');
            if (strcmp(ext, ".json") == 0) {
              // prevents parsing spotify video data that causes duplicates
              if (platform == SPOTIFY && strstr(json_file->d_name, "Video")) {
                continue;
              }
              char json_file_path[MAX_FILENAME_SIZE];
              if (snprintf(json_file_path, MAX_FILENAME_SIZE, "%s/%s",
                           data_dir_path, json_file->d_name) < 0) {
                return EXIT_FAILURE;
              }

              json_to_db(json_file_path, platform);
            }
          }
        }
      }
    }
  }
  return EXIT_SUCCESS;
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
    return EXIT_FAILURE;
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
  return EXIT_SUCCESS;
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
    return EXIT_FAILURE;
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
      return EXIT_FAILURE;
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
        return EXIT_FAILURE;
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
  return EXIT_SUCCESS;
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
      return EXIT_FAILURE;
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
          return EXIT_FAILURE;
        }
        extract(full_name, target);
      }
    }
  }

  closedir(dir);
  add_dir_to_db(target_base, SPOTIFY);
  return EXIT_SUCCESS;
}

int main(void) {
  import_spotify();

  return EXIT_SUCCESS;
}
