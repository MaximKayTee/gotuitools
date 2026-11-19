-- YouTube stream URLs 403 until 5 to 6 s after extraction (yt-dlp
-- "available_at"). ytdl_hook ignores it; hold the load until the first
-- stream URL answers a range request, probed with curl twice a second,
-- WAIT seconds from the end of the extraction at most. Without curl:
-- WAIT.
local WAIT = 8
local EVERY = 0.5
local youtube

-- fixed upstream after 0.41 (mpv PR #18193); inert on newer builds
local maj, min = mp.get_property("mpv-version", ""):match("v(%d+)%.(%d+)")
if not maj or tonumber(maj) > 0 or tonumber(min) > 41 then return end

local function is_yt(path)
    return path:match("^https?://[%w.]*youtube%.com/")
        or path:match("^https?://youtu%.be/")
        or path:match("^ytdl://")
end

-- the first URL of what ytdl_hook opens: a URL, or an EDL of
-- %length%url segments
local function first_url(s)
    local len, pos = s:match("%%(%d+)%%()https?://")
    if len then return s:sub(pos, pos + tonumber(len) - 1) end
    return s:match("^https?://[^;]+")
end

-- before ytdl_hook replaces the name
mp.add_hook("on_load", 5, function()
    youtube = is_yt(mp.get_property("stream-open-filename", "")) ~= nil
end)

mp.add_hook("on_load", 50, function(hook)
    if not youtube then return end
    youtube = false
    local deadline = mp.get_time() + WAIT
    local url = first_url(mp.get_property("stream-open-filename", ""))
    mp.msg.verbose("probing", url and url:sub(1, 60) or "nothing: no URL")
    hook:defer()
    local function rest()
        mp.add_timeout(math.max(deadline - mp.get_time(), 0), function() hook:cont() end)
    end
    if not url then return rest() end
    local function probe()
        mp.command_native_async({
            name = "subprocess", playback_only = false, capture_stdout = true,
            args = {"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "-r", "0-1023", "--max-time", "4", url},
        }, function(ok, res)
            if not ok or not res or res.error_string == "init" then return rest() end
            local code = tonumber(res.stdout or "")
            mp.msg.verbose("HTTP", code, "at", mp.get_time())
            if code and code >= 200 and code < 300 then return hook:cont() end
            if mp.get_time() + EVERY >= deadline then return rest() end
            mp.add_timeout(EVERY, probe)
        end)
    end
    probe()
end)
