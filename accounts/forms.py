from allauth.account.forms import LoginForm
from django.forms import CheckboxInput

INPUT_CLASSES = (
    "w-full rounded-lg border border-stone-300 px-3 py-2 text-sm "
    "focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500"
)


class StyledLoginForm(LoginForm):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        for field in self.fields.values():
            if not isinstance(field.widget, CheckboxInput):
                field.widget.attrs["class"] = INPUT_CLASSES
