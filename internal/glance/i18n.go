package glance

import (
	"encoding/json"
	"html/template"
	"strings"
)

type locale struct {
	Code string

	// Geocoding language code for Open-Meteo (e.g. "en", "pt")
	GeocodingLanguage string

	Months        [12]string
	WeekdaysFull  [7]string
	WeekdaysAbbr  [7]string
	DateSeparator string // between day and month name, e.g. " " or " de "

	CalendarTitle         string
	CalendarBackToCurrent string
	CalendarPreviousMonth string
	CalendarNextMonth     string

	ClockTitle   string
	ClockAM      string
	ClockPM      string
	ClockBehind  string
	ClockAhead   string
	ClockHour    string
	ClockHours   string
	ClockMinutes string
	ClockAnd     string

	WeatherTitle     string
	WeatherFeelsLike string
	WeatherTime12h   [12]string
	WeatherCodes     map[int]string
}

// clientI18n is the subset of locale strings exposed to JavaScript via pageData.
type clientI18n struct {
	Months                [12]string `json:"months"`
	WeekdaysFull          [7]string  `json:"weekdaysFull"`
	WeekdaysAbbr          [7]string  `json:"weekdaysAbbr"`
	DateSeparator         string     `json:"dateSeparator"`
	CalendarBackToCurrent string     `json:"calendarBackToCurrent"`
	CalendarPreviousMonth string     `json:"calendarPreviousMonth"`
	CalendarNextMonth     string     `json:"calendarNextMonth"`
	ClockAM               string     `json:"clockAM"`
	ClockPM               string     `json:"clockPM"`
	ClockBehind           string     `json:"clockBehind"`
	ClockAhead            string     `json:"clockAhead"`
	ClockHour             string     `json:"clockHour"`
	ClockHours            string     `json:"clockHours"`
	ClockMinutes          string     `json:"clockMinutes"`
	ClockAnd              string     `json:"clockAnd"`
}

func (l *locale) toClientI18n() clientI18n {
	return clientI18n{
		Months:                l.Months,
		WeekdaysFull:          l.WeekdaysFull,
		WeekdaysAbbr:          l.WeekdaysAbbr,
		DateSeparator:         l.DateSeparator,
		CalendarBackToCurrent: l.CalendarBackToCurrent,
		CalendarPreviousMonth: l.CalendarPreviousMonth,
		CalendarNextMonth:     l.CalendarNextMonth,
		ClockAM:               l.ClockAM,
		ClockPM:               l.ClockPM,
		ClockBehind:           l.ClockBehind,
		ClockAhead:            l.ClockAhead,
		ClockHour:             l.ClockHour,
		ClockHours:            l.ClockHours,
		ClockMinutes:          l.ClockMinutes,
		ClockAnd:              l.ClockAnd,
	}
}

func (l *locale) clientI18nJSON() template.JS {
	data, err := json.Marshal(l.toClientI18n())
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(data)
}

func (c config) ClientI18nJSON() template.JS {
	if c.locale == nil {
		return localeEn.clientI18nJSON()
	}
	return c.locale.clientI18nJSON()
}

func (l *locale) weatherCodeAsString(code int) string {
	if s, ok := l.WeatherCodes[code]; ok {
		return s
	}
	return ""
}

var weatherCodesEn = map[int]string{
	0:  "Clear Sky",
	1:  "Mainly Clear",
	2:  "Partly Cloudy",
	3:  "Overcast",
	45: "Fog",
	48: "Rime Fog",
	51: "Drizzle",
	53: "Drizzle",
	55: "Drizzle",
	56: "Drizzle",
	57: "Drizzle",
	61: "Rain",
	63: "Moderate Rain",
	65: "Heavy Rain",
	66: "Freezing Rain",
	67: "Freezing Rain",
	71: "Snow",
	73: "Moderate Snow",
	75: "Heavy Snow",
	77: "Snow Grains",
	80: "Rain",
	81: "Moderate Rain",
	82: "Heavy Rain",
	85: "Snow",
	86: "Snow",
	95: "Thunderstorm",
	96: "Thunderstorm",
	99: "Thunderstorm",
}

var weatherCodesPtBR = map[int]string{
	0:  "Céu limpo",
	1:  "Predominantemente limpo",
	2:  "Parcialmente nublado",
	3:  "Nublado",
	45: "Neblina",
	48: "Nevoeiro congelante",
	51: "Garoa",
	53: "Garoa",
	55: "Garoa",
	56: "Garoa",
	57: "Garoa",
	61: "Chuva",
	63: "Chuva moderada",
	65: "Chuva forte",
	66: "Chuva congelante",
	67: "Chuva congelante",
	71: "Neve",
	73: "Neve moderada",
	75: "Neve intensa",
	77: "Grãos de neve",
	80: "Chuva",
	81: "Chuva moderada",
	82: "Chuva forte",
	85: "Neve",
	86: "Neve",
	95: "Tempestade",
	96: "Tempestade",
	99: "Tempestade",
}

var localeEn = &locale{
	Code:              "en",
	GeocodingLanguage: "en",
	Months: [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	},
	WeekdaysFull: [7]string{
		"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday",
	},
	WeekdaysAbbr:  [7]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"},
	DateSeparator: " ",

	CalendarTitle:         "Calendar",
	CalendarBackToCurrent: "Back to current month",
	CalendarPreviousMonth: "Previous month",
	CalendarNextMonth:     "Next month",

	ClockTitle:   "Clock",
	ClockAM:      "AM",
	ClockPM:      "PM",
	ClockBehind:  "behind",
	ClockAhead:   "ahead",
	ClockHour:    "hour",
	ClockHours:   "hours",
	ClockMinutes: "minutes",
	ClockAnd:     "and",

	WeatherTitle:     "Weather",
	WeatherFeelsLike: "Feels like",
	WeatherTime12h:   [12]string{"2am", "4am", "6am", "8am", "10am", "12pm", "2pm", "4pm", "6pm", "8pm", "10pm", "12am"},
	WeatherCodes:     weatherCodesEn,
}

var localePtBR = &locale{
	Code:              "pt-BR",
	GeocodingLanguage: "pt",
	Months: [12]string{
		"janeiro", "fevereiro", "março", "abril", "maio", "junho",
		"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
	},
	WeekdaysFull: [7]string{
		"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado",
	},
	WeekdaysAbbr:  [7]string{"Do", "Se", "Te", "Qa", "Qi", "Sx", "Sa"},
	DateSeparator: " de ",

	CalendarTitle:         "Calendário",
	CalendarBackToCurrent: "Voltar ao mês atual",
	CalendarPreviousMonth: "Mês anterior",
	CalendarNextMonth:     "Próximo mês",

	ClockTitle:   "Relógio",
	ClockAM:      "AM",
	ClockPM:      "PM",
	ClockBehind:  "atrasado",
	ClockAhead:   "adiantado",
	ClockHour:    "hora",
	ClockHours:   "horas",
	ClockMinutes: "minutos",
	ClockAnd:     "e",

	WeatherTitle:     "Clima",
	WeatherFeelsLike: "Sensação de",
	WeatherTime12h:   [12]string{"2 AM", "4 AM", "6 AM", "8 AM", "10 AM", "12 PM", "2 PM", "4 PM", "6 PM", "8 PM", "10 PM", "12 AM"},
	WeatherCodes:     weatherCodesPtBR,
}

var supportedLocales = map[string]*locale{
	"en":    localeEn,
	"pt-br": localePtBR,
}

// defaultLocale is used during widget initialize() before providers are attached.
var defaultLocale = localeEn

func normalizeLanguageCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "en"
	}
	code = strings.ReplaceAll(code, "_", "-")
	parts := strings.Split(code, "-")
	for i := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(parts[i])
		} else {
			parts[i] = strings.ToUpper(parts[i])
		}
	}
	return strings.Join(parts, "-")
}

func resolveLocale(code string) *locale {
	key := strings.ToLower(normalizeLanguageCode(code))
	if l, ok := supportedLocales[key]; ok {
		return l
	}
	return localeEn
}

func isSupportedLanguage(code string) bool {
	if strings.TrimSpace(code) == "" {
		return true
	}
	key := strings.ToLower(normalizeLanguageCode(code))
	_, ok := supportedLocales[key]
	return ok
}
