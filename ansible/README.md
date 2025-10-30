## How to install

```bash
cd ansible
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
pip install argcomplete
activate-global-python-argcomplete
source ~/.zshrc
```

## How to run

### Specify host
```bash
ansible-playbook -i inventory/jail/hosts.ini -l host -u root run.yml -D
```

### localhost
```bash
ansible-playbook -i inventory/jail/hosts.ini -l localhost run.yml -C -D
```
