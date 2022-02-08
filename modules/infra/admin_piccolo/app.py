from base64 import b64decode
import sys
import asyncpg

from modules.core.permissions import is_staff
sys.path.append(".")
sys.path.append("modules/infra/admin_piccolo")
from fastapi import FastAPI
from typing import Callable, Awaitable, Tuple, Dict, List
from starlette.responses import Response, StreamingResponse, RedirectResponse, HTMLResponse
from starlette.requests import Request
from fastapi.responses import ORJSONResponse
from piccolo.engine import engine_finder
from piccolo_admin.endpoints import create_admin
from piccolo_api.crud.endpoints import PiccoloCRUD
from piccolo_api.fastapi.endpoints import FastAPIWrapper
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.routing import Mount
from starlette.types import Scope, Message
from tables import Bot, Reviews, BotTag, User, Vanity, BotListTags, ServerTags
import orjson
import aioredis
from modules.core import redis_ipc_new
from config import bot_logs
from discord import Embed
from piccolo.apps.user.tables import BaseUser
from lynxfall.utils.string import get_token

admin = create_admin(
    [Vanity, User, Bot, BotTag, BotListTags, ServerTags, Reviews], 
    allowed_hosts = ["lynx.fateslist.xyz"], 
    production = True,
)

class Unknown:
    username = "Unknown"

class CustomHeaderMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        # Call the API
        if request.cookies.get("sunbeam-session:warriorcats"):
            request.scope["sunbeam_user"] = orjson.loads(b64decode(request.cookies.get("sunbeam-session:warriorcats")))
        else:
            return RedirectResponse("https://fateslist.xyz/frostpaw/herb?redirect=https://lynx.fateslist.xyz")

        check = await app.state.db.fetchval(
            "SELECT user_id FROM users WHERE user_id = $1 AND api_token = $2", 
            int(request.scope["sunbeam_user"]["user"]["id"]), 
            request.scope["sunbeam_user"]["token"]
        )

        if not check:
            return HTMLResponse("<h1>You do not have permission to access this page</h1>")

        _, perm, _ = await is_staff(None, int(request.scope["sunbeam_user"]["user"]["id"]), 2, redis=app.state.redis)

        if perm < 5:
            return HTMLResponse("<h1>You do not have permission to access this page</h1>")
        
        username = request.scope["sunbeam_user"]["user"]["username"]
        password = get_token(96)

        try:
            await BaseUser.create_user(
                username=username, 
                password=password,
                email=username + "@fateslist.xyz", 
                active=True,
                admin=True
            )

            if request.url.path == "/":
                return HTMLResponse(
                    f"""
                        <h1>Welcome to Lynx</h1>
                        <h2>Credentials for login is <code>{username}</code> and <code>{password}</code></h2>
                        <h3>You will never see this again! Take note somewhere secret, to reset this, go to /reset</h3>
                    """
                )
        except Exception as exc:
            print(exc)
        
        if request.url.path == "/reset":
            await app.state.db.execute("DELETE FROM piccolo_user WHERE username = $1", username)
            return HTMLResponse("Reset creds")

        response = await call_next(request)

        if not response.status_code < 400:
            return response

        try:
            print(request.user.user.username)
        except:
            request.scope["user"] = Unknown()

        if request.url.path.startswith("/api/tables/bots") and request.method == "PATCH":
            print("Got bot edit, sending message")
            path = request.url.path.rstrip("/")
            bot_id = int(path.split("/")[-1])
            print("Got bot id: ", bot_id)
            owner = await app.state.db.fetchval("SELECT owner FROM bot_owner WHERE bot_id = $1", bot_id)
            embed = Embed(
                title = "Bot Edited Via Lynx", 
                description = f"Bot <@{bot_id}> has been edited via Lynx by user {request.user.user.username}", 
                color = 0x00ff00,
                url=f"https://fateslist.xyz/bot/{bot_id}"
            )
            await redis_ipc_new(app.state.redis, "SENDMSG", msg={"content": f"<@{owner}>", "embed": embed.to_dict(), "channel_id": str(bot_logs)})
        return response

admin = CustomHeaderMiddleware(admin)

async def server_error(request, exc):
    return HTMLResponse(content="Error", status_code=exc.status_code)

app = FastAPI(routes=[Mount("/", admin)])

@app.on_event("startup")
async def startup():
    engine = engine_finder()
    app.state.engine = engine
    app.state.redis = aioredis.from_url("redis://localhost:1001", db=1)
    app.state.db = await asyncpg.create_pool()
    await engine.start_connection_pool()

@app.on_event("shutdown")
async def close():
    await app.state.engine.close_connection_pool()
