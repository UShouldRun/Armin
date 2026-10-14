package catalog

import (
  "net/url"
  "os"
  "path/filepath"
  "strings"
)

// regionZones maps an OpenRouter in-region host prefix to the time
// zones of the countries it serves: "eu." decrypts in the EU and
// routes only to EU provider endpoints, "us." the same in the US. A
// new region is a new entry.
var regionZones = map[string][]string{
  "eu": {
    "Europe/Vienna", "Europe/Brussels", "Europe/Sofia", "Europe/Zagreb",
    "Asia/Nicosia", "Asia/Famagusta", "Europe/Nicosia", "Europe/Prague",
    "Europe/Copenhagen", "Europe/Tallinn", "Europe/Helsinki",
    "Europe/Mariehamn", "Europe/Paris", "Europe/Berlin", "Europe/Busingen",
    "Europe/Athens", "Europe/Budapest", "Europe/Dublin", "Europe/Rome",
    "Europe/Riga", "Europe/Vilnius", "Europe/Luxembourg", "Europe/Malta",
    "Europe/Amsterdam", "Europe/Warsaw", "Europe/Lisbon", "Atlantic/Madeira",
    "Atlantic/Azores", "Europe/Bucharest", "Europe/Bratislava",
    "Europe/Ljubljana", "Europe/Madrid", "Africa/Ceuta", "Atlantic/Canary",
    "Europe/Stockholm",
  },
  "us": {
    "America/New_York", "America/Detroit", "America/Kentucky/Louisville",
    "America/Kentucky/Monticello", "America/Indiana/Indianapolis",
    "America/Indiana/Vincennes", "America/Indiana/Winamac",
    "America/Indiana/Marengo", "America/Indiana/Petersburg",
    "America/Indiana/Vevay", "America/Chicago", "America/Indiana/Tell_City",
    "America/Indiana/Knox", "America/Menominee", "America/North_Dakota/Center",
    "America/North_Dakota/New_Salem", "America/North_Dakota/Beulah",
    "America/Denver", "America/Boise", "America/Phoenix",
    "America/Los_Angeles", "America/Anchorage", "America/Juneau",
    "America/Sitka", "America/Metlakatla", "America/Yakutat", "America/Nome",
    "America/Adak", "Pacific/Honolulu", "America/Puerto_Rico",
    "America/St_Thomas", "Pacific/Guam", "Pacific/Saipan", "Pacific/Pago_Pago",
    // Older names still common in $TZ.
    "America/Indianapolis", "America/Fort_Wayne", "America/Louisville",
    "US/Eastern", "US/Central", "US/Mountain", "US/Pacific", "US/Arizona",
    "US/Alaska", "US/Hawaii", "US/Aleutian", "US/Michigan",
    "US/East-Indiana", "US/Indiana-Starke", "US/Samoa",
  },
}

// Regional is base moved onto OpenRouter's in-region host when this
// machine's time zone is in a region OpenRouter has one for:
// "https://openrouter.ai/api/v1" becomes "https://eu.openrouter.ai/api/v1"
// in Lisbon, "https://us.openrouter.ai/api/v1" in Chicago. Anything else — another provider, a host already
// regional, a zone outside every region — comes back unchanged.
func Regional(base string) string { return regional(base, zone("/")) }

// regional is Regional for the time zone tz.
func regional(base, tz string) string {
  u, err := url.Parse(base)
  if err != nil || u.Host != "openrouter.ai" {
    return base
  }

  for prefix, zones := range regionZones {
    for _, z := range zones {
      if z == tz {
        u.Host = prefix + "." + u.Host
        return u.String()
      }
    }
  }

  return base
}

// Global is base back on OpenRouter's global host when it's on an
// in-region one — where a request goes once the region refuses (it
// needs a Business plan). "" when base isn't regional.
func Global(base string) string {
  u, err := url.Parse(base)
  if err != nil {
    return ""
  }

  prefix, host, ok := strings.Cut(u.Host, ".")
  _, regional := regionZones[prefix]
  if !ok || !regional || host != "openrouter.ai" {
    return ""
  }

  u.Host = host

  return u.String()
}

// zone is the IANA name of the machine's time zone, against a
// filesystem rooted at root: $TZ, else /etc/timezone, else where
// /etc/localtime links into zoneinfo. "" when none says.
func zone(root string) string {
  tz := strings.TrimPrefix(os.Getenv("TZ"), ":")
  if tz != "" {
    _, name, ok := strings.Cut(tz, "zoneinfo/")
    if ok {
      return name
    }

    return tz
  }

  data, err := os.ReadFile(filepath.Join(root, "etc/timezone"))
  if err == nil && strings.TrimSpace(string(data)) != "" {
    return strings.TrimSpace(string(data))
  }

  link, err := os.Readlink(filepath.Join(root, "etc/localtime"))
  if err != nil {
    return ""
  }

  _, name, _ := strings.Cut(link, "zoneinfo/")

  return name
}
