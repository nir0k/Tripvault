"""Fill an empty test stand with example trips and ideas through the public API.

The examples are data - the JSON files beside this script - and every one of
them is written the way a person would write it: through the same REST API the
interface calls, so it passes the validation of the build the stand runs. When
the API changes and a fixture no longer fits, the stand says so loudly on the
step that failed, instead of quietly holding data no build would accept.

It runs once, after the backend is healthy, as the administrator the stand is
configured with. A stand that already holds the example accounts is left as it
is, so a restart never doubles the examples.

Only the standard library is used, so the image needs nothing but Python.
"""

import json
import mimetypes
import os
import sys
import time
import urllib.error
import urllib.request
import uuid

API = os.environ.get("TRIPVAULT_API_URL", "http://tripvault-backend:8080").rstrip("/") + "/api/v1"
ADMIN_EMAIL = os.environ.get("TRIPVAULT_ADMIN_EMAIL", "admin@example.com")
ADMIN_PASSWORD = os.environ.get("TRIPVAULT_ADMIN_PASSWORD", "stand-admin-password")
FIXTURES = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures")

# The fields of a stay, a transfer, a place and an expense that only a report
# takes; a plan refuses them, so they are held back until the report exists.
REPORT_ONLY = {"actual_cost_amount", "actual_amount", "report"}

# What a fixture's place, day or trip names beside its own fields: the files
# the seed uploads for it after it exists. The API takes none of them in the
# body that creates the element.
EXTRAS = {"photos", "favorite_photos", "cover_photo", "track", "attachments"}


class Failure(Exception):
    """A request the API refused: which one, and what it answered."""


def log(message):
    """Write one line of the stand's log."""
    print(f"seed: {message}", flush=True)


def load(name):
    """Read a fixture, without the note that explains it."""
    with open(os.path.join(FIXTURES, name), encoding="utf-8") as handle:
        data = json.load(handle)
    if isinstance(data, dict):
        data.pop("_about", None)
    return data


def request(method, path, token=None, body=None):
    """Send one request and return the JSON it answered with, or None.

    Arguments:
        method: the HTTP method.
        path: the path under /api/v1.
        token: the access token of the person the request is made as.
        body: the JSON body, or None.

    Raises:
        Failure: when the API answers with an error, naming the request and the answer.
    """
    data = None if body is None else json.dumps(body).encode("utf-8")
    req = urllib.request.Request(API + path, data=data, method=method)
    req.add_header("Accept", "application/json")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            raw = response.read()
    except urllib.error.HTTPError as error:
        raise Failure(f"{method} {path} answered {error.code}: {error.read().decode('utf-8', 'replace')}") from None
    return json.loads(raw) if raw else None


def upload(path, token, name, data, fields=()):
    """Send one file as a multipart upload, the way the browser does.

    Arguments:
        path: the path under /api/v1.
        token: the access token of the person uploading.
        name: the file's name, which says what kind of file it is.
        data: its bytes.
        fields: (name, value) pairs sent as form fields before the file.

    Raises:
        Failure: when the API refuses the file.
    """
    boundary = "tripvault-seed-" + uuid.uuid4().hex
    body = b""
    for field, value in fields:
        body += (f"--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"\r\n\r\n{value}\r\n").encode("utf-8")
    kind = mimetypes.guess_type(name)[0] or "application/octet-stream"
    body += (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{name}\"\r\n"
             f"Content-Type: {kind}\r\n\r\n").encode("utf-8") + data + f"\r\n--{boundary}--\r\n".encode("utf-8")
    req = urllib.request.Request(API + path, data=body, method="POST")
    req.add_header("Accept", "application/json")
    req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
    req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=120) as response:
            raw = response.read()
    except urllib.error.HTTPError as error:
        raise Failure(f"POST {path} with {name} answered {error.code}: {error.read().decode('utf-8', 'replace')}") from None
    return json.loads(raw) if raw else None


def fixture_file(folder, name):
    """Read one of the files beside the fixtures: a photograph, a track or an attachment."""
    with open(os.path.join(FIXTURES, folder, name), "rb") as handle:
        return handle.read()


def wait_for_backend():
    """Wait until the backend says it is ready, for at most five minutes."""
    ready = API.removesuffix("/api/v1") + "/readyz"
    for _ in range(150):
        try:
            with urllib.request.urlopen(ready, timeout=5):
                return
        except (urllib.error.URLError, OSError):
            time.sleep(2)
    raise Failure("the backend did not become ready")


def login(email, password):
    """Sign in and return the access token."""
    return request("POST", "/auth/login", body={"email": email, "password": password})["access_token"]


def make_accounts(admin):
    """Create the editor and the viewer with the published password.

    An account an administrator creates must change its password on its first
    sign-in, so each is signed in once with a throwaway password and given the
    published one, as its owner would.

    Returns:
        the user identifiers by role, the administrator's as "owner" included.
    """
    fixture = load("accounts.json")
    people = {"owner": request("GET", "/me", admin)["id"]}
    for account in fixture["accounts"]:
        temporary = "temporary-" + fixture["password"]
        created = request("POST", "/admin/users", admin, {
            "email": account["email"], "display_name": account["display_name"], "password": temporary,
        })
        own = login(account["email"], temporary)
        request("POST", "/me/password", own, {"current_password": temporary, "new_password": fixture["password"]})
        people[account["role"]] = created["id"]
    return people, fixture["password"]


def without(fields, *names):
    """Copy a fixture's fields without the named ones."""
    return {key: value for key, value in fields.items() if key not in names}


def place_body(place, people):
    """Turn a fixture's place into the body the API takes, naming who pays and shares."""
    body = without(place, "report", "split", "day", *EXTRAS)
    split = place.get("split")
    if split:
        body["paid_by"] = people[split["paid_by"]]
        shares = [{"user_id": people[share["person"]], "amount": share.get("amount")} for share in split["shares"]]
        body["cost_split"] = "individuals" if any(share["amount"] for share in shares) else "everyone"
        body["cost_shares"] = shares
    return body


def share(admin, trip_id, title, people):
    """Join the editor and the viewer to a trip and hand it a link, whose address is logged."""
    for role in ("editor", "viewer"):
        request("POST", f"/trips/{trip_id}/members", admin, {"user_id": people[role], "role": role})
    link = request("POST", f"/trips/{trip_id}/share-links", admin, {"label": "For the family"})
    log(f"share link of {title!r}: /s#token={link['token']}")


def label_of(item, stays):
    """Name an element of a day as a leg is looked up by: a stay mark by its stay."""
    if item["kind"] == "stay_anchor":
        return stays.get(item.get("stay_id"), "")
    return item["name"]


def write_legs(admin, document_id, legs):
    """Give the named legs their mode, figures, cost and note, or their parts."""
    document = request("GET", f"/documents/{document_id}", admin)
    stays = {stay["id"]: stay["name"] for stay in document["stays"]}
    for wanted in legs:
        day = document["days"][wanted["day"] - 1]
        labels = {item["id"]: label_of(item, stays) for item in day["items"]}
        leg = next((leg for leg in day["legs"] if labels.get(leg["from_item_id"]) == wanted["from"]), None)
        if leg is None:
            raise Failure(f"day {wanted['day']} has no leg leaving {wanted['from']!r}")
        if wanted.get("segments"):
            parts = {
                "segments": [{
                    "mode": part["mode"], "distance_m": None, "duration_s": part.get("duration_s"),
                    "ticket": part["ticket"] - 1 if part.get("ticket") else None, "stop": part.get("stop"),
                } for part in wanted["segments"]],
                "tickets": wanted["tickets"],
            }
            request("PUT", f"/legs/{leg['id']}/segments", admin, parts)
            request("PATCH", f"/legs/{leg['id']}", admin, {"note": wanted.get("note", "")})
            continue
        request("PATCH", f"/legs/{leg['id']}", admin, without(wanted, "day", "from"))


def item_of(document, day_number, name):
    """Find a place or an activity of a document by its day and name."""
    for item in document["days"][day_number - 1]["items"]:
        if item["kind"] != "stay_anchor" and item["name"] == name:
            return item
    raise Failure(f"day {day_number} has no place named {name!r}")


class Gallery:
    """The photographs of one trip, uploaded once each and found by file name."""

    def __init__(self, admin, trip_id, private=()):
        self.admin, self.trip_id, self.private, self.ids = admin, trip_id, set(private), {}

    def id(self, name):
        """Upload a photograph the first time it is named, and return its identifier."""
        if name not in self.ids:
            fields = [("private", "true")] if name in self.private else []
            stored = upload(f"/trips/{self.trip_id}/media", self.admin, name, fixture_file("photos", name), fields)
            self.ids[name] = stored["items"][0]["id"]
        return self.ids[name]

    def link(self, target, target_id, names, favorites=None):
        """Hang photographs on a day or a place, marking a report's favourites among them."""
        body = {"target_type": target, "target_id": target_id, "media_ids": [self.id(name) for name in names]}
        if favorites is not None:
            body["favorite_media_ids"] = [self.id(name) for name in favorites]
        request("PUT", "/media-links", self.admin, body)

    def cover(self, cover):
        """Frame the trip's cover out of one of its photographs."""
        if cover:
            request("PATCH", f"/trips/{self.trip_id}", self.admin,
                    {"cover_media_id": self.id(cover["photo"]), "cover_crop": cover.get("crop")})


def dress_place(admin, gallery, item, extras, report=False):
    """Give a stored place what a fixture adds after it exists: its photographs,
    favourites and cover, its route or recording, and its attachments."""
    if extras.get("photos"):
        gallery.link("item", item["id"], extras["photos"], extras.get("favorite_photos") if report else None)
    if extras.get("cover_photo"):
        request("PATCH", f"/items/{item['id']}", admin, {"cover_media_id": gallery.id(extras["cover_photo"])})
    track = extras.get("track")
    if track:
        track = track if isinstance(track, dict) else {"file": track}
        upload(f"/items/{item['id']}/track", admin, track["file"], fixture_file("tracks", track["file"]))
        if track.get("speed_kmh"):
            request("PATCH", f"/items/{item['id']}/track", admin, {"speed_kmh": track["speed_kmh"]})
    for attachment in extras.get("attachments", []):
        # An attachment is its file's name, or the name with the line describing it.
        attachment = attachment if isinstance(attachment, dict) else {"file": attachment}
        fields = [("description", attachment["description"])] if attachment.get("description") else []
        upload(f"/items/{item['id']}/attachments", admin, attachment["file"],
               fixture_file("attachments", attachment["file"]), fields)


def dress_plan(admin, trip_id, plan_id, trip):
    """Add the plan's files: photographs on its days and places, the cover, the
    routes of its activities and the bookings attached to its places."""
    gallery = Gallery(admin, trip_id)
    document = request("GET", f"/documents/{plan_id}", admin)
    for number, day in enumerate(trip["days"], 1):
        if day.get("photos"):
            gallery.link("day", document["days"][number - 1]["id"], day["photos"])
        for place in day["places"]:
            dress_place(admin, gallery, item_of(document, number, place["name"]), place)
    gallery.cover(trip.get("cover"))


def tag_trip(admin, tags, trip_id, wanted):
    """Put the administrator's tags on a trip, making each tag the first time it is named."""
    if wanted:
        request("PUT", f"/trips/{trip_id}/tags", admin, {"tag_ids": [tag_id(admin, tags, name, color)
                                                                     for name, color in wanted.items()]})


def tag_id(admin, tags, name, color):
    """Return the identifier of one of the administrator's tags, making it when it is new."""
    if name not in tags:
        tags[name] = request("POST", "/tags", admin, {"name": name, "color": color})["id"]
    return tags[name]


def write_plan(admin, plan_id, trip, people):
    """Fill a plan: the days' words, the stays, the transfers, the places, the
    unassigned ideas, the separate expenses and the legs."""
    document = request("GET", f"/documents/{plan_id}", admin)
    days = document["days"]
    for index, day in enumerate(trip["days"]):
        request("PATCH", f"/days/{days[index]['id']}", admin, without(day, "places", "photos", "cover"))
    # The stays come first, so their marks stand in the days before the legs are
    # looked up between the elements of each day.
    for stay in trip.get("stays", []):
        request("POST", f"/documents/{plan_id}/stays", admin, without(stay, *REPORT_ONLY))
    for transfer in trip.get("transfers", []):
        request("POST", f"/documents/{plan_id}/transfers", admin, without(transfer, *REPORT_ONLY))
    for index, day in enumerate(trip["days"]):
        for place in day["places"]:
            request("POST", f"/days/{days[index]['id']}/items", admin, place_body(place, people))
    for place in trip.get("unassigned", []):
        request("POST", f"/documents/{plan_id}/items", admin, place_body(place, people))
    for expense in trip.get("expenses", []):
        body = without(expense, "day", *REPORT_ONLY)
        if expense.get("day"):
            body["day_id"] = days[expense["day"] - 1]["id"]
        request("POST", f"/documents/{plan_id}/expenses", admin, body)
    write_legs(admin, plan_id, trip.get("legs", []))


def write_packing(admin, trip_id, categories, people):
    """Write a plan's packing list, category by category; a nameless one holds
    the items without a category."""
    for category in categories:
        category_id = None
        if category.get("name"):
            packing = request("POST", f"/trips/{trip_id}/packing/categories", admin,
                              {"name": category["name"], "icon": category.get("icon", "other")})
            category_id = packing["categories"][-1]["id"]
        for item in category["items"]:
            body = without(item, "bringer")
            body["category_id"] = category_id
            if item.get("bringer"):
                body["bringer_id"] = people[item["bringer"]]
            request("POST", f"/trips/{trip_id}/packing/items", admin, body)


def write_report(admin, plan_trip_id, trip, people):
    """Copy the plan into a report, the way a person makes one, and write how
    the trip went: the words around the days, every place's outcome, what was
    really spent, the place added on the way and the translation.

    Returns:
        the report's trip.
    """
    report = trip["report"]
    written = request("POST", f"/trips/{plan_trip_id}/reports", admin)
    report_id = written["report_id"]
    languages = ["en"] + ([report["translation"]["lang"]] if report.get("translation") else [])
    request("PATCH", f"/trips/{written['id']}", admin, {"languages": languages})
    request("PATCH", f"/documents/{report_id}", admin,
            {"intro_md": report.get("intro_md", ""), "summary_md": report.get("summary_md", "")})

    document = request("GET", f"/documents/{report_id}", admin)
    days = document["days"]
    for number, notes in report.get("day_notes", {}).items():
        request("PATCH", f"/days/{days[int(number) - 1]['id']}", admin, {"notes_md": notes})
    for number, highlight in report.get("day_highlights", {}).items():
        request("PATCH", f"/days/{days[int(number) - 1]['id']}", admin, {"highlight": highlight})

    # A place is found by its day and name: the same airport starts and ends
    # the trip, and each visit went its own way.
    for number, day in enumerate(trip["days"], 1):
        for place in day["places"]:
            if "report" in place:
                item = item_of(document, number, place["name"])
                request("PATCH", f"/items/{item['id']}", admin, without(place["report"], *EXTRAS))

    spent = {"stays": {}, "transfers": {}, "expenses": {}}
    for kind, key, field in (("stays", "name", "actual_cost_amount"), ("transfers", "name", "actual_cost_amount"),
                             ("expenses", "note", "actual_amount")):
        for entry in trip.get(kind, []):
            if entry.get(field):
                spent[kind][entry[key]] = entry[field]
        for stored in document[kind]:
            amount = spent[kind].get(stored[key])
            if amount:
                request("PATCH", f"/{kind}/{stored['id']}", admin, {field: amount})

    extra = report.get("unplanned")
    if extra:
        body = place_body(without(extra, "report"), people)
        body.update(extra.get("report", {}))
        body["status"] = "unplanned"
        request("POST", f"/days/{days[extra['day'] - 1]['id']}/items", admin, body)

    dress_report(admin, written["id"], report_id, trip)
    if report.get("translation"):
        translate(admin, written["id"], report_id, report["translation"])
    return written


def dress_report(admin, report_trip_id, report_id, trip):
    """Add the report's own files: the photographs of its days and places with
    their favourites, covers and the private one, the recordings that replace
    the plan's routes and the receipts attached to its places. None of the
    plan's pictures or attachments came along with the copy."""
    report = trip["report"]
    gallery = Gallery(admin, report_trip_id, report.get("private_photos", []))
    document = request("GET", f"/documents/{report_id}", admin)
    for number, day in enumerate(trip["days"], 1):
        for place in day["places"]:
            if "report" in place:
                dress_place(admin, gallery, item_of(document, number, place["name"]), place["report"], report=True)
    for number, names in report.get("day_photos", {}).items():
        gallery.link("day", document["days"][int(number) - 1]["id"], names)
    for number, name in report.get("day_covers", {}).items():
        request("PATCH", f"/days/{document['days'][int(number) - 1]['id']}", admin,
                {"cover_media_id": gallery.id(name)})
    gallery.cover(report.get("cover"))


def translate(admin, report_trip_id, report_id, translation):
    """Write the report's words in its second language, leaving out what the
    fixture leaves untranslated, so the fallback to the original shows."""
    document = request("GET", f"/documents/{report_id}", admin)
    entries = []

    def add(target, target_id, field, value):
        if value:
            entries.append({"target_type": target, "target_id": target_id, "field": field, "value": value})

    add("trip", report_trip_id, "title", translation.get("title"))
    add("trip", report_trip_id, "summary", translation.get("summary"))
    add("document", report_id, "intro_md", translation.get("intro_md"))
    add("document", report_id, "summary_md", translation.get("summary_md"))
    for index, day in enumerate(document["days"]):
        number = str(index + 1)
        add("day", day["id"], "title", translation.get("day_titles", {}).get(number))
        add("day", day["id"], "notes_md", translation.get("day_notes", {}).get(number))
        add("day", day["id"], "highlight", translation.get("day_highlights", {}).get(number))
        for item in day["items"]:
            words = translation.get("places", {}).get(item["name"])
            if item["kind"] != "stay_anchor" and words:
                for field in ("name", "description_md", "story_md"):
                    add("item", item["id"], field, words.get(field))
    request("PUT", f"/documents/{report_id}/translations/{translation['lang']}", admin, {"translations": entries})


def write_trip(admin, name, people, tags):
    """Create one example trip from its fixture: the plan with its files, its
    people, link, tags and packing list, and the report when the fixture has one."""
    trip = load(name)
    fields = without(trip, "stays", "transfers", "days", "unassigned", "expenses", "legs", "packing", "report",
                     "completed", "tags", "cover")
    created = request("POST", "/trips", admin, {**fields, "kind": "plan"})
    share(admin, created["id"], trip["title"], people)
    write_plan(admin, created["plan_id"], trip, people)
    dress_plan(admin, created["id"], created["plan_id"], trip)
    tag_trip(admin, tags, created["id"], trip.get("tags"))
    if trip.get("packing"):
        write_packing(admin, created["id"], trip["packing"], people)
    if trip.get("report"):
        report = write_report(admin, created["id"], trip, people)
        share(admin, report["id"], trip["title"] + " (report)", people)
        tag_trip(admin, tags, report["id"], trip["report"].get("tags"))
    # A travelled trip's plan is closed the way its travellers would close it on
    # coming home; its report, a trip of its own, has no state to close.
    if trip.get("completed"):
        request("PATCH", f"/trips/{created['id']}", admin, {"state": "completed"})
    log(f"created {trip['title']!r}")


def write_ideas(admin, tags):
    """Create the administrator's ideas with their photographs and the tags they
    wear, each tag once across the ideas and the trips."""
    fixture = load("ideas.json")
    for idea in fixture["ideas"]:
        created = request("POST", "/ideas", admin, without(idea, "tags", "photos"))
        for photo in idea.get("photos", []):
            upload(f"/ideas/{created['id']}/photos", admin, photo, fixture_file("photos", photo))
        ids = [tag_id(admin, tags, name, fixture["tags"].get(name)) for name in idea.get("tags", [])]
        request("PUT", f"/ideas/{created['id']}/tags", admin, {"tag_ids": ids})
    log(f"created {len(fixture['ideas'])} ideas")


def main():
    """Seed the stand, or leave a seeded one - or one asked to stay empty - as it is."""
    if os.environ.get("TRIPVAULT_SEED", "true").strip().lower() in ("false", "0", "no", "off"):
        log("TRIPVAULT_SEED is off: the stand stays empty")
        return
    wait_for_backend()
    admin = login(ADMIN_EMAIL, ADMIN_PASSWORD)
    emails = {user["email"] for user in request("GET", "/admin/users", admin)["items"]}
    if any(account["email"] in emails for account in load("accounts.json")["accounts"]):
        log("the examples are there already")
        return
    people, password = make_accounts(admin)
    tags = {}
    for name in ("iceland.json", "lisbon.json"):
        write_trip(admin, name, people, tags)
    write_ideas(admin, tags)
    log(f"done: sign in as editor@example.com or viewer@example.com with the password {password!r}")


if __name__ == "__main__":
    try:
        main()
    except Failure as failure:
        log(f"failed: {failure}")
        sys.exit(1)
