package weather

// displayName maps the game's internal environment name to the UI's
// display name, from weather-data.keep.json's environments[].displayName,
// adjusted per the spec (2026-10-05-time-and-weather-design.md):
// "Clear (forest mist)" -> "Forest mist", SwampRain -> "Rain" (already
// the JSON value, listed for clarity), Snow -> "Snow" (not the JSON's
// "Snowing"), SnowStorm -> "Blizzard" (already the JSON value), and the
// Deep North twilight variants folded onto their plain names:
// Twilight_Snow -> "Snow", Twilight_SnowStorm -> "Blizzard",
// Twilight_Clear -> "Clear". Ashlands_storm -> "Ash storm" (not the
// JSON's "Thunderstorm").
var displayName = map[string]string{
	"Clear":               "Clear",
	"Rain":                "Rain",
	"Misty":               "Fog",
	"ThunderStorm":        "Thunderstorm",
	"LightRain":           "Light rain",
	"DeepForest Mist":     "Forest mist",
	"SwampRain":           "Rain",
	"SnowStorm":           "Blizzard",
	"Snow":                "Snow",
	"Heath clear":         "Clear",
	"Mistlands_clear":     "Clear",
	"Mistlands_rain":      "Rain",
	"Mistlands_thunder":   "Thunderstorm",
	"Ashlands_ashrain":    "Ash rain",
	"Ashlands_misty":      "Fog",
	"Ashlands_CinderRain": "Cinder rain",
	"Ashlands_storm":      "Ash storm",
	"Twilight_SnowStorm":  "Blizzard",
	"Twilight_Snow":       "Snow",
	"Twilight_Clear":      "Clear",
}
