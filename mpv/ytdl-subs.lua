-- YouTube's auto-generated captions, added to the subtitle tracks
-- ytdl_hook adds: the video's language when yt-dlp names it and it has
-- captions, else every original one. Title "auto", WebVTT.
local utils = require "mp.utils"

local function primary(lang)
    return type(lang) == "string" and lang:match("^(%a+)") or nil
end

-- after ytdl_hook's extraction
mp.add_hook("on_load", 15, function()
    local res = mp.get_property_native("user-data/mpv/ytdl/json-subprocess-result")
    if type(res) ~= "table" or res.status ~= 0 or type(res.stdout) ~= "string" then return end
    local json = utils.parse_json(res.stdout)
    local caps = type(json) == "table" and json.automatic_captions
    if type(caps) ~= "table" then return end
    local want = primary(json.language)
    local all, own = {}, {}
    for key in pairs(caps) do
        local lang = key:match("^(.+)%-orig$")
        if lang then
            all[#all + 1] = {key = key, lang = lang}
            if want and primary(lang) == want then own[#own + 1] = all[#all] end
        end
    end
    if #own > 0 then all = own end
    table.sort(all, function(a, b) return a.lang < b.lang end)
    for _, c in ipairs(all) do
        local url
        for _, f in ipairs(caps[c.key]) do
            if f.ext == "vtt" and type(f.url) == "string" then url = f.url end
        end
        if url and url:match("^https://") then
            mp.msg.verbose("adding captions", c.lang)
            mp.commandv("sub-add", "edl://!no_clip;!delay_open,media_type=sub,codec=webvtt;%" .. #url .. "%" .. url,
                "auto", "auto", c.lang)
        end
    end
end)
