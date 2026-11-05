import { createClient } from "@supabase/supabase-js";

var url = import.meta.env.VITE_SUPABASE_URL || "https://vilwohwxdwwuetidggkl.supabase.co";
var key = import.meta.env.VITE_SUPABASE_ANON_KEY;

export var supabase = url && key ? createClient(url, key) : null;