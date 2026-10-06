const { App, TerraformStack, TerraformVariable, TerraformOutput, LocalBackend } = require("cdktn");

class Net extends TerraformStack {
  constructor(scope, id) {
    super(scope, id);
    new LocalBackend(this, { path: `${id}.tfstate` });
    const env = new TerraformVariable(this, "env", { type: "string", default: "dev" });
    new TerraformOutput(this, "name", { value: `${id}-${env.value}` });
  }
}

const app = new App();
const vpc = new Net(app, "vpc");
const svc = new Net(app, "app");
svc.addDependency(vpc);
app.synth();
