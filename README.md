# Muzi
## Self-hosted music listening statistics

### Dependencies
- PostgreSQL

### Installation Instructions (for testing and development) \[Only Supports Spotify Imports ATM\]:
1. Clone the repo:<br>```git clone https://github.com/riwiwa/muzi```
2. Copy over all zip archives obtained from Spotify into the ```imports/spotify-data/zip/``` directory.
3. Ensure PostgreSQL is installed and running locally on port 5432.
4. Run the app with:<br>```go run main.go```
5. Navigate to ```localhost:1234/history``` to see your sorted listening history.
6. Comment out ```importsongs.ImportSpotify()``` from ```main.go``` to prevent the app's attempts to import the Spotify data again

### Roadmap:
- Ability to import all listening statistics and scrobbles from: \[In Progress\]
    - lastfm
    - spotify \[Complete\]
    - apple music

- WebUI \[In Progress\]
    - Full listening history with time \[Functional\]
    - Daily, weekly, monthly, yearly, lifetime presets for listening reports
    - Ability to specify a certain point in time from one datetime to another to list data
    - Grid maker (3x3-10x10)
    - Ability to change artist image
- Multi artist scrobbling
- Ability to "sync" offline scrobbles (send from a device to the server)
- Live scrobbling to the server
- Batch scrobble editor
