"""
Fates List Templating System
"""

from .imports import *
from .permissions import *

_templates = Jinja2Templates(directory="templates") # Setup templates folder


# Template class renderer
class templates():
    @staticmethod
    async def TemplateResponse(f, arg_dict, not_error = True):
        guild = client.get_guild(main_server)
        try:
            request = arg_dict["request"]
        except:
            raise KeyError
        status = arg_dict.get("status_code")
        if "user_id" in request.session.keys():
            arg_dict["css"] = request.session.get("user_css")
            try:
                user = guild.get_member(int(request.session["user_id"]))
            except:
                user = None
            state = await db.fetchval("SELECT state FROM users WHERE user_id = $1", int(request.session["user_id"]))
            if (state == enums.UserState.global_ban or state == enums.UserState.ddr_ban) and not_error:
                ban_type = enums.UserState(state).__doc__
                return await templates.e(request, f"You have been {ban_type} banned from Fates List<br/>", status_code = 403)
            if user is not None:
                staff = is_staff(staff_roles, user.roles, 2)
                request.session["staff"] = staff[0], staff[1], staff[2].dict()
            else:
                pass
            arg_dict["staff"] = request.session.get("staff")
            arg_dict["avatar"] = request.session.get("avatar")
            arg_dict["username"] = request.session.get("username")
            arg_dict["user_id"] = int(request.session.get("user_id"))
            arg_dict["user_token"] = request.session.get("user_token")
            try:
                arg_dict["access_token"] = orjson.dumps(request.session.get("access_token")).decode("utf-8")
            except:
                pass
            arg_dict["scopes"] = request.session.get("dscopes_str")
        else:
            arg_dict["staff"] = [False]
        arg_dict["site_url"] = site_url
        arg_dict["data"] = arg_dict.get("data")
        arg_dict["path"] = request.url.path
        arg_dict["enums"] = enums
        arg_dict["len"] = len
        arg_dict["ireplace"] = ireplace
        arg_dict["ireplacem"] = ireplacem
        if status is None:
            ret = _templates.TemplateResponse(f, arg_dict)
        else:
            ret = _templates.TemplateResponse(f, arg_dict, status_code = status)
        if arg_dict.get("csrf_protect"):
            arg_dict["csrf_protect"].set_csrf_cookie(ret)
        return ret

    @staticmethod
    async def error(f, arg_dict, status_code):
        arg_dict["status_code"] = status_code
        return await templates.TemplateResponse(f, arg_dict, not_error = False)

    @staticmethod
    async def e(request, reason: str, status_code: int = 404, *, main: Optional[str] = ""):
        return await templates.error("message.html", {"request": request, "message": main, "context": reason, "retmain": True}, status_code)
