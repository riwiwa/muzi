#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <cjson/cJSON.h>

// TODO:
// - unzip given .zip archives of spotify data
// - web ui
// - enter all json data into postgresql db automatically
// - sql tables: "full history", "artists", "songs", "albums" (see ipad)

int main(void) {
  FILE *fp = fopen("test.json", "r");
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
  char artist[] = "Test";
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
