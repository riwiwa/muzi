# muzi

Self hosted music listening statistics

### Dependencies
- PostgreSQL

### Spotify Import Instructions (for testing and development):
- Navigate to the muzi project root dir
- Create a folder called "spotify-data"
- Inside the new "spotify-data" folder create a folder called "zip"
- Place all zip archives that you obtained from Spotify in this folder.
- Ensure the PostgreSQL server is running locally on port 5432.
- Run muzi.go
- All Spotify tracks that have > 20 second playtime will congregate into the muzi PostgreSQL database

### plans:
- Ability to import all listening statistics and scrobbles from lastfm, spotify, apple music
- daily, weekly, monthly, yearly, lifetime presets for listening reports
- ability to specify a certain point in time from one datetime to another to list data
- grid maker (3x3-10x10)
- multi artist scrobbling
- ability to change artist image
- webUI
- ability to "sync" scrobbles (send from a device to the server)
- live scrobbling to the server
- batch scrobble editor
